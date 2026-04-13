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
	"log/slog"

	parser "github.com/haproxytech/client-native/v6/config-parser"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/haproxy-unified-gateway/hug/reload"
	"github.com/haproxytech/haproxy-unified-gateway/k8s/gate/logging"
)

func (c *clientNative) FrontendCreate(frontend models.Frontend) error {
	configuration, err := c.nativeAPI.Configuration()
	if err != nil {
		return err
	}
	f := &models.Frontend{FrontendBase: frontend.FrontendBase}
	errCreate := configuration.CreateFrontend(f, c.activeTransaction, 0)
	if errCreate != nil {
		// ... maybe it's already existing, so just edit it.
		if err := configuration.EditFrontend(frontend.Name, &frontend, c.activeTransaction, 0); err != nil {
			c.logger.LogAttrs(context.Background(), slog.LevelError, "failed to edit frontend",
				logging.LogAttrError(err),
				slog.String("frontend", frontend.Name),
			)
			return err
		}
	}
	reload.Instance().SetReload("Frontend upserted %s", frontend.Name)

	// Binds
	err = c.BindReplaceAll(parser.Frontends, frontend.Name, frontend.Binds)
	if err != nil {
		return err
	}

	// ACLs
	err = c.ACLReplaceAll(parser.Frontends, frontend.Name, frontend.ACLList)
	if err != nil {
		return err
	}

	// Http Requests
	err = c.HTTPRequestReplaceAll(parser.Frontends, frontend.Name, frontend.HTTPRequestRuleList)
	if err != nil {
		return err
	}

	// TCP Requests
	err = c.TCPRequestReplaceAll(parser.Frontends, frontend.Name, frontend.TCPRequestRuleList)
	if err != nil {
		return err
	}

	// Use backend rules
	err = c.UseBackendReplaceAll(frontend.Name, frontend.BackendSwitchingRuleList)
	if err != nil {
		return err
	}

	// Filters
	err = c.FilterReplaceAll(parser.Frontends, frontend.Name, frontend.FilterList)
	if err != nil {
		return err
	}

	return nil
}

func (c *clientNative) FrontendDelete(frontendName string) error {
	configuration, err := c.nativeAPI.Configuration()
	if err != nil {
		return err
	}
	reload.Instance().SetReload("Frontend deleted %s", frontendName)
	return configuration.DeleteFrontend(frontendName, c.activeTransaction, 0)
}

func (c *clientNative) FrontendsGet() (models.Frontends, error) {
	configuration, err := c.nativeAPI.Configuration()
	if err != nil {
		return nil, err
	}
	// TODO: complete with children
	_, frontends, err := configuration.GetFrontends(c.activeTransaction)

	// Binds
	for _, frontend := range frontends {
		_, binds, err := configuration.GetBinds(string(parser.Frontends), frontend.Name, c.activeTransaction)
		if err != nil {
			return nil, err
		}
		if len(binds) != 0 {
			frontend.Binds = make(map[string]models.Bind)
		}
		for _, bind := range binds {
			if bind != nil {
				frontend.Binds[bind.Name] = *bind
			}
		}
	}
	return frontends, err
}

func (c *clientNative) FrontendGet(frontendName string) (models.Frontend, error) {
	configuration, err := c.nativeAPI.Configuration()
	if err != nil {
		return models.Frontend{}, err
	}
	// TODO: complete with children
	_, frontend, err := configuration.GetFrontend(frontendName, c.activeTransaction)
	if err != nil {
		return models.Frontend{}, err
	}

	// Binds
	_, binds, err := configuration.GetBinds(string(parser.Frontends), frontend.Name, c.activeTransaction)
	if err != nil {
		return models.Frontend{}, err
	}
	if len(binds) != 0 {
		frontend.Binds = make(map[string]models.Bind)
	}
	for _, bind := range binds {
		if bind != nil {
			frontend.Binds[bind.Name] = *bind
		}
	}

	return *frontend, err
}

func (c *clientNative) FrontendEdit(frontend models.Frontend) error {
	configuration, err := c.nativeAPI.Configuration()
	if err != nil {
		return err
	}
	f := &models.Frontend{FrontendBase: frontend.FrontendBase}
	if err := configuration.EditFrontend(frontend.Name, f, c.activeTransaction, 0); err != nil {
		c.logger.LogAttrs(context.Background(), slog.LevelError, "failed to edit frontend",
			logging.LogAttrError(err),
			slog.String("frontend", frontend.Name),
		)
		return err
	}

	reload.Instance().SetReload("Frontend upserted %s", frontend.Name)

	// Binds
	err = c.BindReplaceAll(parser.Frontends, frontend.Name, frontend.Binds)
	if err != nil {
		return err
	}

	// ACLs
	err = c.ACLReplaceAll(parser.Frontends, frontend.Name, frontend.ACLList)
	if err != nil {
		return err
	}

	// Http Requests
	err = c.HTTPRequestReplaceAll(parser.Frontends, frontend.Name, frontend.HTTPRequestRuleList)
	if err != nil {
		return err
	}

	// TCP Requests
	err = c.TCPRequestReplaceAll(parser.Frontends, frontend.Name, frontend.TCPRequestRuleList)
	if err != nil {
		return err
	}

	// Use backend rules
	err = c.UseBackendReplaceAll(frontend.Name, frontend.BackendSwitchingRuleList)
	if err != nil {
		return err
	}

	// Filters
	err = c.FilterReplaceAll(parser.Frontends, frontend.Name, frontend.FilterList)
	if err != nil {
		return err
	}

	return nil
}
