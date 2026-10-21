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
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/haproxy/structured"
)

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
