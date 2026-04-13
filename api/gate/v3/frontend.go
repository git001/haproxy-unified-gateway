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
	"github.com/haproxytech/client-native/v6/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:metadata:annotations="client-native.haproxy.org/version=v6.2.5"

// Frontend is a specification for a Frontend resource
type Frontend struct {
	Spec              FrontendSpec `json:"spec"`
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// FrontendSpec defines the desired state of Frontend
type FrontendSpec struct {
	ParentRef  *LocalParentRef `json:"parentRef,omitempty"`
	FilterList models.Filters  `json:"filter_list,omitempty"`
}

// LocalParentRef references a Gateway by name and optional namespace
type LocalParentRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FrontendList is a list of Frontend resources
type FrontendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Frontend `json:"items"`
}

// DeepCopyInto deepcopying the receiver into out. in must be non-nil.
func (in *FrontendSpec) DeepCopyInto(out *FrontendSpec) {
	if in.ParentRef != nil {
		ref := *in.ParentRef
		out.ParentRef = &ref
	}
	if in.FilterList != nil {
		out.FilterList = make(models.Filters, len(in.FilterList))
		for i, f := range in.FilterList {
			if f != nil {
				var fc models.Filter
				b, err := f.MarshalBinary()
				if err == nil {
					_ = fc.UnmarshalBinary(b)
				}
				out.FilterList[i] = &fc
			}
		}
	}
}
