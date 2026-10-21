// Copyright 2019 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package v3

import (
	"encoding/json"

	"github.com/haproxytech/client-native/v6/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:metadata:annotations="client-native.haproxy.org/version=v6.2.5"

// Backend is a specification for a Backend resource
type Backend struct {
	Spec              BackendSpec `json:"spec"`
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// BackendSpec defines the desired state of Backend
type BackendSpec struct {
	models.Backend `json:",inline"`
	// ConfigSnippet holds raw lines of HAProxy configuration appended to the
	// backend, for the directives the other fields do not support. It is only
	// used when HUG runs with --enable-backend-config-snippets, since it allows
	// any HAProxy configuration.
	// +optional
	ConfigSnippet string `json:"config_snippet,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackendList is a list of Backend resources
type BackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Backend `json:"items"`
}

// MarshalJSON implements json.Marshaler for BackendSpec.
// This is needed because the embedded models.Backend has its own MarshalJSON,
// which would be promoted and would omit the config_snippet field.
func (in BackendSpec) MarshalJSON() ([]byte, error) {
	backendJSON, err := in.Backend.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if in.ConfigSnippet == "" {
		return backendJSON, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(backendJSON, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = make(map[string]json.RawMessage)
	}
	m["config_snippet"], err = json.Marshal(in.ConfigSnippet)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// UnmarshalJSON implements json.Unmarshaler for BackendSpec.
// This is needed because the embedded models.Backend has its own UnmarshalJSON,
// which would be promoted and would swallow the config_snippet field.
func (in *BackendSpec) UnmarshalJSON(data []byte) error {
	if err := in.Backend.UnmarshalJSON(data); err != nil {
		return err
	}
	var shadow struct {
		ConfigSnippet string `json:"config_snippet"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	in.ConfigSnippet = shadow.ConfigSnippet
	return nil
}

// DeepCopyInto deepcopying  the receiver into out. in must be non-nil.
func (in *BackendSpec) DeepCopyInto(out *BackendSpec) {
	b, err := in.Backend.MarshalBinary()
	if err == nil {
		_ = out.Backend.UnmarshalBinary(b)
	}
	out.ConfigSnippet = in.ConfigSnippet
}
