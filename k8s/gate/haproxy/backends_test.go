// Copyright 2025 HAProxy Technologies LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package haproxy

import (
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	v3 "github.com/haproxytech/haproxy-unified-gateway/api/gate/v3"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/store"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/tree"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// testBackendExtractGVK resolves the HUG Backend object type to its GVK, enough
// for IsFilterExtensionRefKindSupported to recognise a cr-backend ExtensionRef.
func testBackendExtractGVK(client.Object) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: v3.GroupName, Version: "v3", Kind: "Backend"}
}

// newTestBackendMgr builds a minimal HaproxyConfMgrImpl exercising only the
// Backend CR stores and the GVK extractor.
func newTestBackendMgr(backendCRs map[k8stypes.NamespacedName]*v3.Backend, ingressBackendCRs map[k8stypes.NamespacedName]*unstructured.Unstructured) *HaproxyConfMgrImpl {
	if backendCRs == nil {
		backendCRs = map[k8stypes.NamespacedName]*v3.Backend{}
	}
	if ingressBackendCRs == nil {
		ingressBackendCRs = map[k8stypes.NamespacedName]*unstructured.Unstructured{}
	}
	mgr := &HaproxyConfMgrImpl{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		controllerStore: &tree.ControllerStore{
			ClusterStore: &store.ClusterStore{
				BackendCRs:        backendCRs,
				IngressBackendCRs: ingressBackendCRs,
			},
		},
	}
	mgr.params.extractGVK = testBackendExtractGVK
	return mgr
}

// crBackendRef builds an HTTPBackendRef carrying a cr-backend ExtensionRef
// pointing at the named Backend CR (HUG group).
func crBackendRef(name string) gatewayv1.HTTPBackendRef {
	return gatewayv1.HTTPBackendRef{
		Filters: []gatewayv1.HTTPRouteFilter{{
			Type: gatewayv1.HTTPRouteFilterExtensionRef,
			ExtensionRef: &gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(v3.GroupName),
				Kind:  "Backend",
				Name:  gatewayv1.ObjectName(name),
			},
		}},
	}
}

func TestIngressCRBackendReferencesHugCR(t *testing.T) {
	nsName := k8stypes.NamespacedName{Namespace: "default", Name: "my-backend"}

	t.Run("name resolves only to a HUG Backend CR is a mismatch", func(t *testing.T) {
		mgr := newTestBackendMgr(map[k8stypes.NamespacedName]*v3.Backend{nsName: {}}, nil)
		got, mismatch := mgr.ingressCRBackendReferencesHugCR(crBackendRef(nsName.Name), nsName.Namespace)
		if !mismatch {
			t.Fatal("expected a type mismatch")
		}
		if got != nsName.Name {
			t.Errorf("offending name: want %q, got %q", nsName.Name, got)
		}
	})

	t.Run("name resolves to a foreign Ingress Backend CR is fine", func(t *testing.T) {
		mgr := newTestBackendMgr(nil, map[k8stypes.NamespacedName]*unstructured.Unstructured{nsName: newIngressBackendUnstructured("http", "roundrobin")})
		if _, mismatch := mgr.ingressCRBackendReferencesHugCR(crBackendRef(nsName.Name), nsName.Namespace); mismatch {
			t.Fatal("expected no mismatch when the foreign CR exists")
		}
	})

	t.Run("name present in both stores is fine (foreign wins)", func(t *testing.T) {
		mgr := newTestBackendMgr(
			map[k8stypes.NamespacedName]*v3.Backend{nsName: {}},
			map[k8stypes.NamespacedName]*unstructured.Unstructured{nsName: newIngressBackendUnstructured("http", "roundrobin")},
		)
		if _, mismatch := mgr.ingressCRBackendReferencesHugCR(crBackendRef(nsName.Name), nsName.Namespace); mismatch {
			t.Fatal("expected no mismatch when the foreign CR also exists")
		}
	})

	t.Run("unknown name is not a mismatch (dangling reference)", func(t *testing.T) {
		mgr := newTestBackendMgr(nil, nil)
		if _, mismatch := mgr.ingressCRBackendReferencesHugCR(crBackendRef(nsName.Name), nsName.Namespace); mismatch {
			t.Fatal("expected no mismatch for an unknown reference")
		}
	})

	t.Run("no ExtensionRef filter is not a mismatch", func(t *testing.T) {
		mgr := newTestBackendMgr(map[k8stypes.NamespacedName]*v3.Backend{nsName: {}}, nil)
		if _, mismatch := mgr.ingressCRBackendReferencesHugCR(gatewayv1.HTTPBackendRef{}, nsName.Namespace); mismatch {
			t.Fatal("expected no mismatch without an ExtensionRef filter")
		}
	})
}

// TestWarnMistypedIngressBackendsKeepsBackend verifies that an Ingress-origin
// backend whose cr-backend references a HUG Backend CR (wrong type) is NOT set
// aside: the backend stays in Upserted (it is still built with defaults, the
// reference merely does not resolve in the Ingress store), matching the Gateway
// API route behaviour.
func TestWarnMistypedIngressBackendsKeepsBackend(t *testing.T) {
	const ns = "ingress"
	const backendName = "hug_ingress_nginx-svc_80_abc"
	crNsName := k8stypes.NamespacedName{Namespace: ns, Name: "be-ingress"}
	// A synthetic (ingress-origin) owner route; the "ing:" prefix marks it synthetic.
	syntheticOwner := client.ObjectKey{Namespace: ns, Name: "ing:web:0"}

	mgr := newTestBackendMgr(
		map[k8stypes.NamespacedName]*v3.Backend{crNsName: {}}, // HUG CR present
		nil, // foreign Ingress Backend CR store empty -> type mismatch
	)
	mgr.backendOwners = BackendReferencedBy{
		owners: map[string]map[BackendOwnerType]map[client.ObjectKey]int64{
			backendName: {BackendOwnerTypeHTTPRoute: {syntheticOwner: 1}},
		},
	}
	mgr.backendsImpactedInCycle = BackendsImpactedInCycle{
		Upserted: map[string]map[client.ObjectKey]BackendImpactedInCycle{
			backendName: {syntheticOwner: {BackendRef: crBackendRef("be-ingress")}},
		},
		Deleted:      map[string]struct{}{},
		Unreferenced: map[string]struct{}{},
	}

	mgr.warnMistypedIngressBackends()

	if _, ok := mgr.backendsImpactedInCycle.Upserted[backendName]; !ok {
		t.Fatal("expected the mistyped Ingress backend to remain in Upserted (built with defaults), not set aside")
	}
}

func TestResolveBackendCR(t *testing.T) {
	nsName := k8stypes.NamespacedName{Namespace: "default", Name: "my-backend"}

	const (
		fromGatewayAPI = false
		fromIngress    = true
	)

	t.Run("Gateway API route resolves the HUG Backend CR only", func(t *testing.T) {
		want := &v3.Backend{}
		want.Spec.Mode = "http"
		mgr := newTestBackendMgr(
			map[k8stypes.NamespacedName]*v3.Backend{nsName: want},
			// A foreign CR with the same key must never be consulted for a Gateway
			// API route.
			map[k8stypes.NamespacedName]*unstructured.Unstructured{nsName: newIngressBackendUnstructured("tcp", "leastconn")},
		)

		got, ok := mgr.resolveBackendCR(nsName, fromGatewayAPI)
		if !ok {
			t.Fatal("expected the native Backend CR to be found")
		}
		if got != want {
			t.Fatalf("expected the native Backend CR pointer, got a different one (mode=%q)", got.Spec.Mode)
		}
	})

	t.Run("Ingress resolves the foreign Backend CR only, converted on the fly", func(t *testing.T) {
		mgr := newTestBackendMgr(
			nil,
			map[k8stypes.NamespacedName]*unstructured.Unstructured{nsName: newIngressBackendUnstructured("http", "roundrobin")},
		)

		got, ok := mgr.resolveBackendCR(nsName, fromIngress)
		if !ok {
			t.Fatal("expected the foreign Ingress Backend CR to be resolved")
		}
		if got.Spec.Mode != "http" {
			t.Errorf("Mode: want %q, got %q", "http", got.Spec.Mode)
		}
		// Balance is a nested pointer field of client-native's models.Backend:
		// this proves the JSON round-trip populates non-scalar fields correctly.
		if got.Spec.Balance == nil || got.Spec.Balance.Algorithm == nil {
			t.Fatal("expected Balance.Algorithm to be populated")
		}
		if *got.Spec.Balance.Algorithm != "roundrobin" {
			t.Errorf("Balance.Algorithm: want %q, got %q", "roundrobin", *got.Spec.Balance.Algorithm)
		}
	})

	t.Run("Ingress cannot borrow a HUG Backend CR", func(t *testing.T) {
		// Only a HUG Backend CR exists under the name; an Ingress reference must not
		// resolve it (type confusion across API groups).
		mgr := newTestBackendMgr(
			map[k8stypes.NamespacedName]*v3.Backend{nsName: {}},
			nil,
		)
		if _, ok := mgr.resolveBackendCR(nsName, fromIngress); ok {
			t.Fatal("expected an Ingress not to resolve a HUG Backend CR")
		}
	})

	t.Run("Gateway API route cannot borrow a foreign Ingress Backend CR", func(t *testing.T) {
		mgr := newTestBackendMgr(
			nil,
			map[k8stypes.NamespacedName]*unstructured.Unstructured{nsName: newIngressBackendUnstructured("http", "roundrobin")},
		)
		if _, ok := mgr.resolveBackendCR(nsName, fromGatewayAPI); ok {
			t.Fatal("expected a Gateway API route not to resolve a foreign Ingress Backend CR")
		}
	})

	t.Run("unknown reference is not found", func(t *testing.T) {
		mgr := newTestBackendMgr(nil, nil)
		if _, ok := mgr.resolveBackendCR(nsName, fromIngress); ok {
			t.Fatal("expected no Backend CR to be found")
		}
	})
}

// newIngressBackendUnstructured builds a generic (unstructured) foreign
// kubernetes-ingress Backend CR whose spec mirrors client-native's models.Backend.
func newIngressBackendUnstructured(mode, balanceAlgorithm string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "ingress.v3.haproxy.org/v3",
		"kind":       "Backend",
		"metadata": map[string]any{
			"name":      "my-backend",
			"namespace": "default",
		},
		"spec": map[string]any{
			"mode": mode,
			"balance": map[string]any{
				"algorithm": balanceAlgorithm,
			},
		},
	}}
}

// A redirect rule dropped by match validation must not create or keep a
// redirect pseudo-backend, mirroring what the route-map layer already does.
func TestRedirectRuleWithInvalidMatchesKeepsNoBackend(t *testing.T) {
	redirectFilters := []gatewayv1.HTTPRouteFilter{{
		Type:            gatewayv1.HTTPRouteFilterRequestRedirect,
		RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{},
	}}
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}

	newMgr := func() *HaproxyConfMgrImpl {
		mgr := newTestBackendMgr(nil, nil)
		mgr.params.LinkID = "hug"
		mgr.backendOwners = NewBackendOwners()
		mgr.backendsImpactedInCycle = BackendsImpactedInCycle{
			Upserted:     map[string]map[client.ObjectKey]BackendImpactedInCycle{},
			Deleted:      map[string]struct{}{},
			Unreferenced: map[string]struct{}{},
		}
		return mgr
	}
	newRoute := func(matchesValid bool) *tree.HTTPRoute {
		return &tree.HTTPRoute{
			K8sResource: &gatewayv1.HTTPRoute{
				Namespace: "ns", Name: "route", Generation: 1,
			},
			Valid: matchesValid,
			Rules: []*tree.HTTPRouteRule{{
				K8sResource:  gatewayv1.HTTPRouteRule{Filters: redirectFilters},
				CheckFilters: tree.CheckResult{Valid: true},
				CheckMatches: tree.CheckResult{Valid: matchesValid},
				Valid:        matchesValid,
			}},
		}
	}

	t.Run("invalid matches create no redirect backend", func(t *testing.T) {
		mgr := newMgr()
		if err := mgr.upsertHTTPRouteBackends(routeKey, newRoute(false)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mgr.backendsImpactedInCycle.Upserted) != 0 {
			t.Errorf("expected no upserted backend, got %v", mgr.backendsImpactedInCycle.Upserted)
		}
		if owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey); len(owned) != 0 {
			t.Errorf("expected no owned backend, got %v", owned)
		}
	})

	t.Run("previously created redirect backend is released", func(t *testing.T) {
		mgr := newMgr()
		beName := mgr.getRedirectBackendName(redirectFilters)
		if err := mgr.backendOwners.addHTTPRoute(beName, routeKey, newRoute(true).K8sResource); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := mgr.upsertHTTPRouteBackends(routeKey, newRoute(false)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := mgr.backendsImpactedInCycle.Unreferenced[beName]; !ok {
			t.Errorf("expected %q to be unreferenced", beName)
		}
		if owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey); len(owned) != 0 {
			t.Errorf("expected the route to own no backend, got %v", owned)
		}
	})

	t.Run("valid matches still create the redirect backend", func(t *testing.T) {
		mgr := newMgr()
		beName := mgr.getRedirectBackendName(redirectFilters)
		if err := mgr.upsertHTTPRouteBackends(routeKey, newRoute(true)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := mgr.backendsImpactedInCycle.Upserted[beName][routeKey]; !ok {
			t.Errorf("expected the redirect backend %q to be upserted", beName)
		}
	})
}

// newNoBackendRefsTestMgr builds a manager with initialized owners and cycle
// tracking, enough to exercise upsertHTTPRouteBackends end to end.
func newNoBackendRefsTestMgr() *HaproxyConfMgrImpl {
	mgr := newTestBackendMgr(nil, nil)
	mgr.params.LinkID = "hug"
	mgr.params.BackendNameTemplate = "{{ .LINK_ID }}_{{ .SERVICE_NAMESPACE }}_{{ .SERVICE_NAME }}_{{ .SERVICE_PORT }}_{{ .FILTER_HASH }}"
	mgr.backendOwners = NewBackendOwners()
	mgr.backendsImpactedInCycle = BackendsImpactedInCycle{
		Upserted:     map[string]map[client.ObjectKey]BackendImpactedInCycle{},
		Deleted:      map[string]struct{}{},
		Unreferenced: map[string]struct{}{},
	}
	return mgr
}

// backendRefsRule builds a valid rule with the given filters, matches and refs.
func backendRefsRule(filters []gatewayv1.HTTPRouteFilter, matches []gatewayv1.HTTPRouteMatch,
	refs []gatewayv1.HTTPBackendRef, valid bool,
) *tree.HTTPRouteRule {
	rule := &tree.HTTPRouteRule{
		K8sResource: gatewayv1.HTTPRouteRule{
			Filters:     filters,
			Matches:     matches,
			BackendRefs: refs,
		},
		CheckBackendRef: utils.NewKeyMap[gatewayv1.BackendObjectReference, tree.CheckResult](utils.BackendObjectReferenceToKey),
		CheckFilters:    tree.CheckResult{Valid: valid},
		CheckMatches:    tree.CheckResult{Valid: valid},
		Valid:           valid,
	}
	for _, ref := range refs {
		rule.CheckBackendRef.Set(ref.BackendObjectReference, tree.CheckResult{Valid: valid})
	}
	return rule
}

func noBackendRefsRoute(rules ...*tree.HTTPRouteRule) *tree.HTTPRoute {
	return &tree.HTTPRoute{
		K8sResource: &gatewayv1.HTTPRoute{Namespace: "ns", Name: "route", Generation: 1},
		Valid:       true,
		Rules:       rules,
	}
}

func corsFilter() gatewayv1.HTTPRouteFilter {
	return gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterCORS,
		CORS: &gatewayv1.HTTPCORSFilter{AllowOrigins: []gatewayv1.CORSOrigin{"*"}},
	}
}

func blockedPrefixMatches() []gatewayv1.HTTPRouteMatch {
	return []gatewayv1.HTTPRouteMatch{{
		Path: &gatewayv1.HTTPPathMatch{
			Type:  new(gatewayv1.PathMatchPathPrefix),
			Value: new("/blocked"),
		},
	}}
}

// A valid backendless rule must be upserted and owned as a response-only
// pseudo-backend, whether BackendRefs is nil or empty.
func TestNoBackendRefsRuleUpsert(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}

	for name, refs := range map[string][]gatewayv1.HTTPBackendRef{
		"nil backend refs":   nil,
		"empty backend refs": {},
	} {
		t.Run(name, func(t *testing.T) {
			mgr := newNoBackendRefsTestMgr()
			filters := []gatewayv1.HTTPRouteFilter{corsFilter()}
			matches := blockedPrefixMatches()
			route := noBackendRefsRoute(backendRefsRule(filters, matches, refs, true))

			beName := mgr.getNoBackendRefsBackendName(filters, "/blocked")
			if !strings.HasPrefix(beName, "hug_no_backend_refs_") {
				t.Fatalf("unexpected pseudo-backend name %q", beName)
			}

			if err := mgr.upsertHTTPRouteBackends(routeKey, route); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			impacted, ok := mgr.backendsImpactedInCycle.Upserted[beName][routeKey]
			if !ok {
				t.Fatalf("expected the pseudo-backend %q to be upserted, got %v", beName, mgr.backendsImpactedInCycle.Upserted)
			}
			if !impacted.IsNoBackendRefs || impacted.IsRedirect {
				t.Errorf("expected a no-backend-refs pseudo-backend, got %+v", impacted)
			}
			if impacted.MatchPrefix != "/blocked" {
				t.Errorf("MatchPrefix: want /blocked, got %q", impacted.MatchPrefix)
			}
			if !reflect.DeepEqual(impacted.RuleFilters, filters) {
				t.Errorf("RuleFilters not preserved: %+v", impacted.RuleFilters)
			}
			if _, owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey)[beName]; !owned {
				t.Errorf("expected the route to own the pseudo-backend %q", beName)
			}
		})
	}
}

// The pseudo-backend name must separate rules whose filters or match prefix
// differ, since both shape the generated rules.
func TestNoBackendRefsBackendNameUniqueness(t *testing.T) {
	mgr := newNoBackendRefsTestMgr()
	cors := corsFilter()

	first := mgr.getNoBackendRefsBackendName(nil, "")
	second := mgr.getNoBackendRefsBackendName(nil, "")
	if first != second {
		t.Fatal("same filters and prefix must produce the same name")
	}
	if mgr.getNoBackendRefsBackendName([]gatewayv1.HTTPRouteFilter{cors}, "/a") ==
		mgr.getNoBackendRefsBackendName([]gatewayv1.HTTPRouteFilter{cors}, "/b") {
		t.Error("different match prefixes must produce different names")
	}
	other := corsFilter()
	other.CORS.AllowOrigins = []gatewayv1.CORSOrigin{"https://example.com"}
	if mgr.getNoBackendRefsBackendName([]gatewayv1.HTTPRouteFilter{cors}, "/a") ==
		mgr.getNoBackendRefsBackendName([]gatewayv1.HTTPRouteFilter{other}, "/a") {
		t.Error("different filters must produce different names")
	}
}

// The terminal http-request return must come last: a filter direct-response
// (CORS preflight 204) runs first. Default status is 404, compat is 500.
func TestNoBackendRefsBackendTerminalRule(t *testing.T) {
	for _, test := range []struct {
		name   string
		compat bool
		status int64
	}{
		{name: "default status is 404", status: 404},
		{name: "GWAPICompat status is 500", compat: true, status: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			mgr := newNoBackendRefsTestMgr()
			mgr.params.GWAPICompat = test.compat
			filters := []gatewayv1.HTTPRouteFilter{corsFilter()}

			be := mgr.newNoBackendRefsBackend("be", nil, filters, "/blocked")
			if be.Mode != "http" {
				t.Errorf("Mode: want http, got %q", be.Mode)
			}

			rules := be.HTTPRequestRuleList
			if len(rules) != 2 {
				t.Fatalf("expected a CORS preflight return plus the terminal return, got %d rules", len(rules))
			}
			// Filter direct-response first: CORS preflight.
			if rules[0].Type != "return" || rules[0].ReturnStatusCode == nil || *rules[0].ReturnStatusCode != 204 {
				t.Errorf("expected the CORS preflight 204 return first, got %+v", rules[0])
			}
			if rules[0].CondTest == "" || !strings.Contains(rules[0].CondTest, "method OPTIONS") {
				t.Errorf("expected the preflight return to stay conditional, got %q", rules[0].CondTest)
			}
			// Terminal return last, unconditional, with the configured status.
			last := rules[len(rules)-1]
			if last.Type != "return" {
				t.Errorf("terminal rule type: want return, got %q", last.Type)
			}
			if last.ReturnStatusCode == nil || *last.ReturnStatusCode != test.status {
				t.Errorf("terminal status: want %d, got %+v", test.status, last.ReturnStatusCode)
			}
			if last.Cond != "" || last.CondTest != "" {
				t.Errorf("terminal return must be unconditional, got cond %q %q", last.Cond, last.CondTest)
			}
		})
	}
}

// Redirect-only rules with no backendRefs keep their redirect pseudo-backend
// and must not produce a no-backend-refs pseudo-backend.
func TestNoBackendRefsRedirectExemption(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}
	redirectFilters := []gatewayv1.HTTPRouteFilter{{
		Type:            gatewayv1.HTTPRouteFilterRequestRedirect,
		RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{},
	}}

	mgr := newNoBackendRefsTestMgr()
	route := noBackendRefsRoute(backendRefsRule(redirectFilters, nil, nil, true))
	if err := mgr.upsertHTTPRouteBackends(routeKey, route); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantRedirect := mgr.getRedirectBackendName(redirectFilters)
	if _, ok := mgr.backendsImpactedInCycle.Upserted[wantRedirect][routeKey]; !ok {
		t.Errorf("expected the redirect backend %q to be upserted", wantRedirect)
	}
	for beName := range mgr.backendsImpactedInCycle.Upserted {
		if strings.Contains(beName, "_no_backend_refs_") {
			t.Errorf("redirect rule must not create a no-backend-refs backend, got %q", beName)
		}
	}
	for beName := range mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey) {
		if strings.Contains(beName, "_no_backend_refs_") {
			t.Errorf("redirect rule must not own a no-backend-refs backend, got %q", beName)
		}
	}
}

// The pseudo-backend is released when the rule gains backendRefs, and deleted
// with the route.
func TestNoBackendRefsRelease(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}
	matches := blockedPrefixMatches()
	refs := []gatewayv1.HTTPBackendRef{{
		Name: "svc", Port: new(gatewayv1.PortNumber(80)),
	}}

	t.Run("transition to backendRefs releases the pseudo backend", func(t *testing.T) {
		mgr := newNoBackendRefsTestMgr()
		beName := mgr.getNoBackendRefsBackendName(nil, "/blocked")
		if err := mgr.upsertHTTPRouteBackends(routeKey, noBackendRefsRoute(backendRefsRule(nil, matches, nil, true))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := mgr.upsertHTTPRouteBackends(routeKey, noBackendRefsRoute(backendRefsRule(nil, matches, refs, true))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := mgr.backendsImpactedInCycle.Unreferenced[beName]; !ok {
			t.Errorf("expected %q to be unreferenced after the transition", beName)
		}
		owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey)
		if _, ok := owned[beName]; ok {
			t.Errorf("expected the route not to own %q anymore", beName)
		}
		if len(owned) != 1 {
			t.Errorf("expected exactly one owned backend after the transition, got %v", owned)
		}
	})

	t.Run("route deletion deletes the pseudo backend", func(t *testing.T) {
		mgr := newNoBackendRefsTestMgr()
		route := noBackendRefsRoute(backendRefsRule(nil, matches, nil, true))
		beName := mgr.getNoBackendRefsBackendName(nil, "/blocked")
		if err := mgr.upsertHTTPRouteBackends(routeKey, route); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := mgr.onDeletedHTTPRoute(routeKey, route); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := mgr.backendsImpactedInCycle.Deleted[beName]; !ok {
			t.Errorf("expected %q to be deleted with the route", beName)
		}
		if owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey); len(owned) != 0 {
			t.Errorf("expected the route to own no backend, got %v", owned)
		}
	})
}

// Rules with valid backendRefs keep the normal service-backed backends.
func TestNoBackendRefsNormalRefsUnchanged(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}
	mgr := newNoBackendRefsTestMgr()
	refs := []gatewayv1.HTTPBackendRef{{
		Name: "svc", Port: new(gatewayv1.PortNumber(80)),
	}}

	if err := mgr.upsertHTTPRouteBackends(routeKey, noBackendRefsRoute(backendRefsRule(nil, blockedPrefixMatches(), refs, true))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want, err := mgr.getBackendName(k8stypes.NamespacedName{Namespace: "ns", Name: "svc"}, 80, "_")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	impacted, ok := mgr.backendsImpactedInCycle.Upserted[want][routeKey]
	if !ok {
		t.Fatalf("expected the normal backend %q to be upserted, got %v", want, mgr.backendsImpactedInCycle.Upserted)
	}
	if impacted.IsNoBackendRefs || impacted.IsRedirect {
		t.Errorf("normal backend must not be flagged as pseudo, got %+v", impacted)
	}
	for beName := range mgr.backendsImpactedInCycle.Upserted {
		if strings.Contains(beName, "_no_backend_refs_") {
			t.Errorf("backendRefs must not create a no-backend-refs backend, got %q", beName)
		}
	}
}

// Under compatibility, a rule with invalid backendRefs must own the
// response-only backend for its traffic share; valid sibling refs keep their
// own backend. Without the flag, the whole rule is dropped as before.
func TestInvalidBackendRefsRuleUnderCompat(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}
	matches := blockedPrefixMatches()
	refs := []gatewayv1.HTTPBackendRef{
		{Name: "good", Port: new(gatewayv1.PortNumber(80))},
		{Name: "bad", Port: new(gatewayv1.PortNumber(80))},
	}

	for _, compat := range []bool{false, true} {
		t.Run(map[bool]string{false: "compat off drops the rule", true: "compat on answers 500"}[compat], func(t *testing.T) {
			mgr := newNoBackendRefsTestMgr()
			mgr.params.GWAPICompat = compat
			rule := backendRefsRule(nil, matches, refs, true)
			rule.CheckBackendRef.Set(refs[1].BackendObjectReference, tree.CheckResult{Valid: false})
			rule.Valid = false

			if err := mgr.upsertHTTPRouteBackends(routeKey, noBackendRefsRoute(rule)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			good, err := mgr.getBackendName(k8stypes.NamespacedName{Namespace: "ns", Name: "good"}, 80, "_")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			respond := mgr.getNoBackendRefsBackendName(nil, "/blocked")

			owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey)
			if !compat {
				if len(owned) != 0 {
					t.Fatalf("expected no owned backend, got %v", owned)
				}
				return
			}
			if _, ok := owned[good]; !ok {
				t.Errorf("valid ref backend %q must stay owned, got %v", good, owned)
			}
			if _, ok := owned[respond]; !ok {
				t.Errorf("route must own the response-only backend %q, got %v", respond, owned)
			}
			impacted, ok := mgr.backendsImpactedInCycle.Upserted[respond][routeKey]
			if !ok {
				t.Fatalf("expected the response-only backend %q to be upserted, got %v", respond, mgr.backendsImpactedInCycle.Upserted)
			}
			if !impacted.IsNoBackendRefs || impacted.IsRedirect {
				t.Errorf("expected a response-only pseudo-backend, got %+v", impacted)
			}
		})
	}
}

// Invalid backendless rules must not be handled at all.
func TestNoBackendRefsInvalidRuleNotHandled(t *testing.T) {
	routeKey := k8stypes.NamespacedName{Namespace: "ns", Name: "route"}
	mgr := newNoBackendRefsTestMgr()
	route := noBackendRefsRoute(backendRefsRule(nil, blockedPrefixMatches(), nil, false))

	if err := mgr.upsertHTTPRouteBackends(routeKey, route); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mgr.backendsImpactedInCycle.Upserted) != 0 {
		t.Errorf("expected no upserted backend, got %v", mgr.backendsImpactedInCycle.Upserted)
	}
	if owned := mgr.backendOwners.getBackendsReferencedByHTTPRoute(routeKey); len(owned) != 0 {
		t.Errorf("expected no owned backend, got %v", owned)
	}
}
