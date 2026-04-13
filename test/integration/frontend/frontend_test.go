// Copyright 2025 HAProxy Technologies LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package frontend

import (
	"path"

	"github.com/haproxytech/haproxy-unified-gateway/test/integration/utils"
)

// Test_Frontend_AllFrontends verifies that a Frontend CR with no parentRef (nil) applies
// its FilterList to all HAProxy frontends.
func (s *FrontendSuite) Test_Frontend_AllFrontends() {
	fixturePath := path.Join(utils.GetCRDFixturePath(), "all-frontends")
	s.CreateFixtures(fixturePath, nil)
	defer s.CleanupFixtures(fixturePath, nil)

	s.expectFilterCount(httpFrontendName, 1)
}

// Test_Frontend_SpecificGateway verifies that a Frontend CR with a parentRef targeting a
// specific Gateway applies its FilterList to the corresponding HAProxy frontend.
func (s *FrontendSuite) Test_Frontend_SpecificGateway() {
	fixturePath := path.Join(utils.GetCRDFixturePath(), "specific-gateway")
	s.CreateFixtures(fixturePath, nil)
	defer s.CleanupFixtures(fixturePath, nil)

	s.expectFilterCount(httpFrontendName, 1)
}

// Test_Frontend_DynamicUpdate verifies that updating a Frontend CR causes HAProxy to
// apply the new FilterList.
// Step 1: apply v1 (1 compression filter), verify applied.
// Step 2: apply v2 (empty filter_list), verify filters removed.
func (s *FrontendSuite) Test_Frontend_DynamicUpdate() {
	fixturePath := path.Join(utils.GetCRDFixturePath(), "dynamic-update")

	s.CreateFixtures(fixturePath, []string{"frontend-cr-v1.yaml"})
	defer s.CleanupFixtures(fixturePath, []string{"frontend-cr-v1.yaml"})

	s.expectFilterCount(httpFrontendName, 1)

	// Applying v2 (same CR name, empty filter_list) triggers an update reconcile.
	s.CreateFixtures(fixturePath, []string{"frontend-cr-v2.yaml"})

	s.expectFilterCount(httpFrontendName, 0)
}

// Test_Frontend_Delete verifies that deleting a Frontend CR causes HAProxy to remove
// the filters from the affected frontend.
func (s *FrontendSuite) Test_Frontend_Delete() {
	fixturePath := path.Join(utils.GetCRDFixturePath(), "delete")

	s.CreateFixtures(fixturePath, nil)
	s.expectFilterCount(httpFrontendName, 1)

	s.CleanupFixtures(fixturePath, nil)
	s.expectFilterCount(httpFrontendName, 0)
}
