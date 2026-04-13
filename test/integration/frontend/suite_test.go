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
	"fmt"
	"path"
	"testing"
	"time"

	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/haproxy-unified-gateway/test/integration/base"
	"github.com/haproxytech/haproxy-unified-gateway/test/integration/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/suite"
)

const (
	timeout  = time.Second * 15
	interval = time.Second * 1

	// httpFrontendName is the HAProxy frontend created for the HTTP listener on port 8080.
	httpFrontendName = "http_8080"

	// hugConfNamespace is the namespace required by the controller for HugConf CRs.
	// Must match hugConfNsName.Namespace in base/inttest.go.
	hugConfNamespace = "test"
)

type FrontendSuite struct {
	base.BaseSuite
}

func TestFrontendSuite(t *testing.T) {
	suite.Run(t, new(FrontendSuite))
}

func (s *FrontendSuite) SetupSuite() {
	s.BaseSuite.SetupSuite("", 0)

	// Create the "test" namespace required by the controller.
	err := utils.CreateRuntimeObject(s.Test().Ctx, s.Test().Client, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: hugConfNamespace},
	}, true)
	s.Require().NoError(err)

	// Create GatewayClass and Gateway so that HUG generates the http_8080 HAProxy frontend.
	basePath := path.Join(utils.GetCRDFixturePath(), "base")
	s.CreateFixtures(basePath, nil)

	// Wait until the HAProxy frontend exists before running any test.
	s.Require().True(
		utils.WaitFor(s.Test().Ctx, interval, timeout, func() bool {
			_, err := s.Test().HaproxyClient.FrontendGet(httpFrontendName)
			return err == nil
		}),
		"timed out waiting for HAProxy frontend %s", httpFrontendName,
	)
}

func (s *FrontendSuite) TearDownSuite() {
	basePath := path.Join(utils.GetCRDFixturePath(), "base")
	s.CleanupFixtures(basePath, nil)

	_ = utils.DeleteRuntimeObject(s.Test().Ctx, s.Test().Client, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: hugConfNamespace},
	}, false)
	s.BaseSuite.TearDownSuite()
}

// expectFilterCount waits until the named HAProxy frontend has exactly n filters.
func (s *FrontendSuite) expectFilterCount(frontendName string, n int) {
	var got models.Filters
	s.Eventually(func() bool {
		fe, err := s.Test().HaproxyClient.FrontendGet(frontendName)
		if err != nil {
			return false
		}
		got = fe.FilterList
		return len(fe.FilterList) == n
	}, timeout, interval, fmt.Sprintf("expected %d filter(s) on frontend %q, got %v", n, frontendName, got))
}
