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
	"slices"
	"testing"

	"github.com/haproxytech/client-native/v6/models"
	v3 "github.com/haproxytech/haproxy-unified-gateway/api/gate/v3"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/haproxy/structured"
	apiv1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func snippetCR(snippet string) *v3.Backend {
	be := &v3.Backend{}
	be.Spec.ConfigSnippet = snippet
	return be
}

// snippetBackendRef builds an HTTPBackendRef to the Service svc, with an
// ExtensionRef filter per name, "Override" and "Append" being MergeType ones.
func snippetBackendRef(svc string, names ...string) gatewayv1.HTTPBackendRef {
	ref := gatewayv1.HTTPBackendRef{}
	ref.Name = gatewayv1.ObjectName(svc)
	for _, name := range names {
		kind := gatewayv1.Kind("Backend")
		if name == "Override" || name == "Append" {
			kind = "MergeType"
		}
		ref.Filters = append(ref.Filters, gatewayv1.HTTPRouteFilter{
			Type: gatewayv1.HTTPRouteFilterExtensionRef,
			ExtensionRef: &gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(v3.GroupName),
				Kind:  kind,
				Name:  gatewayv1.ObjectName(name),
			},
		})
	}
	return ref
}

func TestConfigSnippetLines(t *testing.T) {
	got := configSnippetLines("\n  filter-config cloudauth enabled profile p host h \n\n\thttp-request deny\n")
	want := []string{"filter-config cloudauth enabled profile p host h", "http-request deny"}
	if !slices.Equal(got, want) {
		t.Errorf("want %q, got %q", want, got)
	}
	if got := configSnippetLines(" \n "); got != nil {
		t.Errorf("want no line, got %q", got)
	}
}

func TestMergeWithBackendCRsConfigSnippet(t *testing.T) {
	ns := "team-a"
	crs := map[k8stypes.NamespacedName]*v3.Backend{
		{Namespace: ns, Name: "a"}:     snippetCR("line-a1\nline-a2"),
		{Namespace: ns, Name: "b"}:     snippetCR("line-b"),
		{Namespace: ns, Name: "plain"}: tuningCR("http", "leastconn"),
		{Namespace: ns, Name: "svc"}:   snippetCR("line-svc"),
	}
	tests := []struct {
		name       string
		ref        gatewayv1.HTTPBackendRef
		annotation string
		want       []string
	}{
		{name: "no CR", ref: snippetBackendRef("echo")},
		{name: "one CR", ref: snippetBackendRef("echo", "a"), want: []string{"line-a1", "line-a2"}},
		{name: "appended by default", ref: snippetBackendRef("echo", "a", "b"), want: []string{"line-a1", "line-a2", "line-b"}},
		{name: "kept by a CR without snippet", ref: snippetBackendRef("echo", "a", "plain"), want: []string{"line-a1", "line-a2"}},
		{name: "replaced in Override mode", ref: snippetBackendRef("echo", "a", "Override", "b"), want: []string{"line-b"}},
		{name: "appended again in Append mode", ref: snippetBackendRef("echo", "Override", "a", "Append", "b"), want: []string{"line-a1", "line-a2", "line-b"}},
		{name: "replaced by the Service CR", ref: snippetBackendRef("echo", "a"), annotation: "svc", want: []string{"line-svc"}},
		{name: "kept by a Service CR without snippet", ref: snippetBackendRef("echo", "a"), annotation: "plain", want: []string{"line-a1", "line-a2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestBackendMgr(crs, nil)
			mgr.controllerStore.ClusterStore.Services = map[k8stypes.NamespacedName]*apiv1.Service{
				{Namespace: ns, Name: "echo"}: annotatedService(ns, "echo", tt.annotation),
			}
			got, errs := mgr.mergeWithBackendCRs(tt.ref, &models.Backend{}, ns, false)
			if err := errs.Result(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNewBackendConfigSnippetOptIn(t *testing.T) {
	crs := map[k8stypes.NamespacedName]*v3.Backend{{Namespace: "ns", Name: "a"}: snippetCR("line-a")}
	for _, enabled := range []bool{false, true} {
		mgr := newTestBackendMgr(crs, nil)
		mgr.params.EnableBackendConfigSnippets = enabled
		_, got, err := mgr.newBackend("be", nil, snippetBackendRef("echo", "a"), nil, "", nil, nil, "ns", true, false)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		if enabled {
			want = []string{"line-a"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("enabled=%t: want %q, got %q", enabled, want, got)
		}
	}
}

func TestUpsertBackendConfigSnippet(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Configuration{structured: structured.NewStructuredConf()}
	be := func() *models.Backend {
		return &models.Backend{BackendBase: models.BackendBase{Name: "be", Mode: "http"}}
	}
	// upsert upserts the backend with snippet, and returns the diffs.
	upsert := func(snippet []string) structured.Structured {
		t.Helper()
		c.resetDiffs()
		if err := c.upsertBackend(logger, be(), snippet); err != nil {
			t.Fatal(err)
		}
		if c.diffs.Created.Backends["be"] != nil {
			return c.diffs.Created
		}
		return c.diffs.Updated
	}

	d := upsert([]string{"line-a"})
	if got, ok := d.BackendConfigSnippets["be"]; !ok || !slices.Equal(got, []string{"line-a"}) {
		t.Fatalf("create: snippet not in the diff: %q", got)
	}
	if d = upsert([]string{"line-a"}); d.Backends["be"] != nil {
		t.Fatal("same backend and snippet: unexpected diff")
	}
	d = upsert([]string{"line-b"})
	if got := d.BackendConfigSnippets["be"]; d.Backends["be"] == nil || !slices.Equal(got, []string{"line-b"}) {
		t.Fatalf("snippet change: want an update with the new snippet, got %q", got)
	}
	d = upsert(nil)
	if got, ok := d.BackendConfigSnippets["be"]; d.Backends["be"] == nil || !ok || len(got) != 0 {
		t.Fatalf("snippet removal: want an update with an empty snippet, got %q", got)
	}

	c.resetDiffs()
	if err := c.deleteBackend(logger, "be"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.structured.BackendConfigSnippets["be"]; ok {
		t.Fatal("snippet of the deleted backend still stored")
	}
}
