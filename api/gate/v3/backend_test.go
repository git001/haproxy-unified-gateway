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
package v3

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBackendSpecJSON(t *testing.T) {
	data := []byte(`{"spec":{"balance":{"algorithm":"leastconn"},"config_snippet":"line-a\nline-b"}}`)

	var be Backend
	if err := json.Unmarshal(data, &be); err != nil {
		t.Fatal(err)
	}
	if be.Spec.ConfigSnippet != "line-a\nline-b" {
		t.Fatalf("config_snippet not decoded: %q", be.Spec.ConfigSnippet)
	}
	if be.Spec.Balance == nil || be.Spec.Balance.Algorithm == nil || *be.Spec.Balance.Algorithm != "leastconn" {
		t.Fatalf("backend fields not decoded: %+v", be.Spec.Balance)
	}

	out, err := json.Marshal(be.Spec)
	if err != nil {
		t.Fatal(err)
	}
	var back BackendSpec
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.ConfigSnippet != be.Spec.ConfigSnippet || back.Balance == nil {
		t.Fatalf("round trip lost fields: %s", out)
	}

	out, err = json.Marshal(BackendSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "config_snippet") {
		t.Fatalf("empty config_snippet encoded: %s", out)
	}
}
