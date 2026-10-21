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
package api

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clientnative "github.com/haproxytech/client-native/v6"
	"github.com/haproxytech/client-native/v6/configuration"
	cfgoptions "github.com/haproxytech/client-native/v6/configuration/options"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/client-native/v6/options"
)

// newTestClient returns a client on a configuration file with no backend, in
// a started transaction.
func newTestClient(t *testing.T) *clientNative {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(cfgFile, []byte("global\n\ndefaults haproxytech\n  mode http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// client-native only asks the HAProxy binary for its version here, the
	// transaction is not committed, so the configuration is not validated
	haproxyBin := filepath.Join(dir, "haproxy")
	if err := os.WriteFile(haproxyBin, []byte("#!/bin/sh\necho 'HAProxy version 3.2.0 2025/05/28 - https://haproxy.org/'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	confClient, err := configuration.New(context.Background(), cfgoptions.ConfigurationFile(cfgFile),
		cfgoptions.TransactionsDir(dir), cfgoptions.HAProxyBin(haproxyBin))
	if err != nil {
		t.Fatal(err)
	}
	nativeAPI, err := clientnative.New(context.Background(), options.Configuration(confClient))
	if err != nil {
		t.Fatal(err)
	}
	c := &clientNative{nativeAPI: nativeAPI, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := c.APIStartTransaction(); err != nil {
		t.Fatal(err)
	}
	return c
}

// snippetOf returns the config snippet of the backend name in the transaction,
// and the configuration as written.
func snippetOf(t *testing.T, c *clientNative, name string) ([]string, string) {
	t.Helper()
	config, err := c.nativeAPI.Configuration()
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.GetParser(c.activeTransaction)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := BackendConfigSnippet(p, name)
	if err != nil {
		t.Fatal(err)
	}
	return lines, p.String()
}

func TestBackendConfigSnippet(t *testing.T) {
	c := newTestClient(t)
	be := models.Backend{BackendBase: models.BackendBase{Name: "be1", Mode: "http"}}
	filter := "filter-config cloudauth enabled profile s3 host s3.example.com"

	// created with a snippet
	if err := c.BackendCreate(be, ConfigSnippet{Lines: []string{filter, "http-request set-var(txn.a) str(b)"}, Set: true}); err != nil {
		t.Fatal(err)
	}
	lines, cfg := snippetOf(t, c, "be1")
	if !slices.Equal(lines, []string{filter, "http-request set-var(txn.a) str(b)"}) {
		t.Fatalf("snippet after create: %q", lines)
	}
	if !strings.Contains(cfg, "###_config-snippet_### BEGIN\n  "+filter+"\n") {
		t.Fatalf("snippet not written in the backend:\n%s", cfg)
	}

	// edited without the snippet, as for a server update: the snippet is kept
	be.Balance = &models.Balance{Algorithm: new("leastconn")}
	if err := c.BackendEdit(be, ConfigSnippet{}); err != nil {
		t.Fatal(err)
	}
	if lines, cfg = snippetOf(t, c, "be1"); len(lines) != 2 || !strings.Contains(cfg, "balance leastconn") {
		t.Fatalf("snippet or backend change lost after edit: %q\n%s", lines, cfg)
	}

	// edited with another snippet: it is replaced
	if err := c.BackendEdit(be, ConfigSnippet{Lines: []string{filter}, Set: true}); err != nil {
		t.Fatal(err)
	}
	if lines, _ = snippetOf(t, c, "be1"); !slices.Equal(lines, []string{filter}) {
		t.Fatalf("snippet after replace: %q", lines)
	}

	// edited with an empty snippet: it is removed
	if err := c.BackendEdit(be, ConfigSnippet{Set: true}); err != nil {
		t.Fatal(err)
	}
	if lines, cfg = snippetOf(t, c, "be1"); lines != nil || strings.Contains(cfg, "config-snippet") {
		t.Fatalf("snippet not removed: %q\n%s", lines, cfg)
	}

	// created again over the existing backend without snippet: none is added
	if err := c.BackendCreate(be, ConfigSnippet{}); err != nil {
		t.Fatal(err)
	}
	if lines, _ = snippetOf(t, c, "be1"); lines != nil {
		t.Fatalf("unexpected snippet: %q", lines)
	}
}
