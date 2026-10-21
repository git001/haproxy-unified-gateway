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
package opt

import "github.com/haproxytech/haproxy-unified-gateway/k8s/gate/config"

// EnableBackendConfigSnippets writes the config_snippet field of the Backend
// CRs into the backends they are attached to, for the HAProxy directives the
// other fields do not support. A snippet allows any HAProxy configuration, so
// it is ignored unless this option is set: only enable it when the users
// allowed to create Backend CRs are trusted with the HAProxy configuration.
func EnableBackendConfigSnippets() func(o *config.Configuration) error {
	return func(o *config.Configuration) error {
		o.HaproxyParams.EnableBackendConfigSnippets = true
		return nil
	}
}
