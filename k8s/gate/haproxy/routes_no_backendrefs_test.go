// Copyright 2026 HAProxy Technologies LLC
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
	"testing"

	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/haproxy/storage/maps"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/tree"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/utils"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func noBackendRefsMapFixture() (*RouteMgrImpl, *tree.HTTPRoute, []*maps.MapFileState) {
	mgr := newTestBackendMgr(nil, nil)
	mgr.params.LinkID = "hug"
	mgr.params.BackendNameTemplate = "{{ .LINK_ID }}_{{ .SERVICE_NAMESPACE }}_{{ .SERVICE_NAME }}_{{ .SERVICE_PORT }}_{{ .FILTER_HASH }}"
	route := &tree.HTTPRoute{
		K8sResource: &gatewayv1.HTTPRoute{Namespace: "ns", Name: "route"},
		Valid:       true,
	}
	mapFiles := []*maps.MapFileState{
		maps.NewMapFileState("", "exact", mgr.logger),
		maps.NewMapFileState("", "prefix", mgr.logger),
		maps.NewMapFileState("", "regex", mgr.logger),
	}
	return &RouteMgrImpl{topManager: mgr}, route, mapFiles
}

func routeMapRule(match gatewayv1.HTTPRouteMatch, refs []gatewayv1.HTTPBackendRef) *tree.HTTPRouteRule {
	rule := &tree.HTTPRouteRule{
		K8sResource: gatewayv1.HTTPRouteRule{
			Matches:     []gatewayv1.HTTPRouteMatch{match},
			BackendRefs: refs,
		},
		CheckBackendRef: utils.NewKeyMap[gatewayv1.BackendObjectReference, tree.CheckResult](utils.BackendObjectReferenceToKey),
		CheckFilters:    tree.CheckResult{Valid: true},
		CheckMatches:    tree.CheckResult{Valid: true},
		Valid:           true,
	}
	for _, ref := range refs {
		rule.CheckBackendRef.Set(ref.BackendObjectReference, tree.CheckResult{Valid: true})
	}
	return rule
}

func applyHTTPRouteMaps(t *testing.T, mgr *RouteMgrImpl, route *tree.HTTPRoute, files []*maps.MapFileState) {
	t.Helper()
	origin := maps.ResourceOrigin{Namespace: route.K8sResource.Namespace, Name: route.K8sResource.Name}
	require.NoError(t, mgr.onUpsertedHTTPRoute(origin, "l1/r1", route, files[0], files[1], files[2]))
	for _, file := range files {
		file.ProcessMapFiles()
	}
}

func TestNoBackendRefsRouteMapPaths(t *testing.T) {
	for _, refs := range []struct {
		name string
		refs []gatewayv1.HTTPBackendRef
	}{
		{name: "omitted"},
		{name: "empty", refs: []gatewayv1.HTTPBackendRef{}},
	} {
		for index, pathType := range []gatewayv1.PathMatchType{
			gatewayv1.PathMatchExact,
			gatewayv1.PathMatchPathPrefix,
			gatewayv1.PathMatchRegularExpression,
		} {
			t.Run(refs.name+"/"+string(pathType), func(t *testing.T) {
				mgr, route, files := noBackendRefsMapFixture()
				match := gatewayv1.HTTPRouteMatch{Path: &gatewayv1.HTTPPathMatch{Type: new(pathType), Value: new("/blocked")}}
				route.Rules = []*tree.HTTPRouteRule{routeMapRule(match, refs.refs)}
				applyHTTPRouteMaps(t, mgr, route, files)

				backend := mgr.topManager.getNoBackendRefsBackendName(nil, extractFirstPathPrefix([]gatewayv1.HTTPRouteMatch{match}))
				desired := newDesiredBackendsMaps()
				_, key := desired.resolveEntry("l1/r1", match)
				for i, file := range files {
					if i == index {
						require.Contains(t, file.Entries, key)
						require.Equal(t, backend, maps.BuildRouteValue(file.Entries[key].DesiredValue))
					} else {
						require.Empty(t, file.Entries)
					}
				}
			})
		}
	}
}

func TestNoBackendRefsRouteMapPrecedence(t *testing.T) {
	refs := []gatewayv1.HTTPBackendRef{{
		Name: "fallback", Port: new(gatewayv1.PortNumber(80)),
	}}
	for _, conditional := range []bool{false, true} {
		t.Run(map[bool]string{false: "first identical rule wins", true: "conditional rule remains a candidate"}[conditional], func(t *testing.T) {
			mgr, route, files := noBackendRefsMapFixture()
			match := gatewayv1.HTTPRouteMatch{}
			if conditional {
				match.Method = new(gatewayv1.HTTPMethodPost)
				match.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "x-block", Value: "yes"}}
				match.QueryParams = []gatewayv1.HTTPQueryParamMatch{{Name: "blocked", Value: "true"}}
			}
			route.Rules = []*tree.HTTPRouteRule{
				routeMapRule(match, nil),
				routeMapRule(gatewayv1.HTTPRouteMatch{}, refs),
			}
			applyHTTPRouteMaps(t, mgr, route, files)
			entry := files[1].Entries[maps.EntryKey{Hostname: "l1/r1", Path: "/"}]
			require.NotNil(t, entry)
			backend := mgr.topManager.getNoBackendRefsBackendName(nil, "")
			want := backend
			if conditional {
				conds, err := maps.NormalizeHTTPRouteMatch(match)
				require.NoError(t, err)
				fallback, err := mgr.topManager.getBackendName(k8stypes.NamespacedName{Namespace: "ns", Name: "fallback"}, 80, "_")
				require.NoError(t, err)
				want = "~1;" + conds.Encode() + ">" + backend + ";>" + fallback
			}
			require.Equal(t, want, maps.BuildRouteValue(entry.DesiredValue))
		})
	}
}

func TestNoBackendRefsRouteMapRedirectAndInvalidRules(t *testing.T) {
	for _, test := range []struct {
		name     string
		redirect bool
		valid    bool
	}{
		{name: "redirect", redirect: true, valid: true},
		{name: "invalid redirect", redirect: true},
		{name: "invalid backendless rule"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mgr, route, files := noBackendRefsMapFixture()
			rule := routeMapRule(gatewayv1.HTTPRouteMatch{}, nil)
			rule.Valid = test.valid
			rule.CheckMatches.Valid = test.valid
			if test.redirect {
				rule.K8sResource.Filters = []gatewayv1.HTTPRouteFilter{{
					Type: gatewayv1.HTTPRouteFilterRequestRedirect, RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{},
				}}
			}
			route.Rules = []*tree.HTTPRouteRule{rule}
			applyHTTPRouteMaps(t, mgr, route, files)
			if test.valid {
				entry := files[1].Entries[maps.EntryKey{Hostname: "l1/r1", Path: "/"}]
				require.NotNil(t, entry)
				require.Equal(t, mgr.topManager.getRedirectBackendName(rule.K8sResource.Filters), maps.BuildRouteValue(entry.DesiredValue))
			} else {
				for _, file := range files {
					require.Empty(t, file.Entries)
				}
			}
		})
	}
}

// Under compatibility, a rule with an invalid backendRef keeps its
// match: its traffic share routes to the response-only backend while valid
// sibling refs keep their own backend. Without the flag the rule disappears.
func TestInvalidBackendRefsRouteMapUnderCompat(t *testing.T) {
	refs := []gatewayv1.HTTPBackendRef{
		{Name: "good", Port: new(gatewayv1.PortNumber(80))},
		{Name: "bad", Port: new(gatewayv1.PortNumber(80))},
	}
	for _, compat := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[compat], func(t *testing.T) {
			mgr, route, files := noBackendRefsMapFixture()
			mgr.topManager.params.GWAPICompat = compat
			rule := routeMapRule(gatewayv1.HTTPRouteMatch{}, refs)
			rule.CheckBackendRef.Set(refs[1].BackendObjectReference, tree.CheckResult{Valid: false})
			rule.Valid = false
			route.Rules = []*tree.HTTPRouteRule{rule}
			applyHTTPRouteMaps(t, mgr, route, files)

			if !compat {
				require.Empty(t, files[1].Entries)
				return
			}
			entry := files[1].Entries[maps.EntryKey{Hostname: "l1/r1", Path: "/"}]
			require.NotNil(t, entry)
			good, err := mgr.topManager.getBackendName(k8stypes.NamespacedName{Namespace: "ns", Name: "good"}, 80, "_")
			require.NoError(t, err)
			respond := mgr.topManager.getNoBackendRefsBackendName(nil, "")
			targets := map[string]struct{}{}
			for _, v := range entry.DesiredValue {
				targets[v.ValueName] = struct{}{}
			}
			require.Contains(t, targets, good)
			require.Contains(t, targets, respond)
		})
	}
}

func TestNoBackendRefsRouteMapUpdates(t *testing.T) {
	mgr, route, files := noBackendRefsMapFixture()
	refs := []gatewayv1.HTTPBackendRef{{
		Name: "fallback", Port: new(gatewayv1.PortNumber(80)),
	}}
	backend, err := mgr.topManager.getBackendName(k8stypes.NamespacedName{Namespace: "ns", Name: "fallback"}, 80, "_")
	require.NoError(t, err)
	for _, step := range []struct {
		refs []gatewayv1.HTTPBackendRef
		want string
	}{
		{refs: refs, want: backend},
		{want: mgr.topManager.getNoBackendRefsBackendName(nil, "")},
		{refs: refs, want: backend},
	} {
		route.Rules = []*tree.HTTPRouteRule{routeMapRule(gatewayv1.HTTPRouteMatch{}, step.refs)}
		applyHTTPRouteMaps(t, mgr, route, files)
		entry := files[1].Entries[maps.EntryKey{Hostname: "l1/r1", Path: "/"}]
		require.NotNil(t, entry)
		require.Equal(t, step.want, maps.BuildRouteValue(entry.DesiredValue))
		for _, file := range files {
			file.Reset()
		}
	}
	origin := maps.ResourceOrigin{Namespace: "ns", Name: "route"}
	require.NoError(t, mgr.onDeletedHTTPRoute(origin, route, files[0], files[1], files[2]))
	files[1].ProcessMapFiles()
	require.Empty(t, maps.BuildRouteValue(files[1].Entries[maps.EntryKey{Hostname: "l1/r1", Path: "/"}].DesiredValue))
}
