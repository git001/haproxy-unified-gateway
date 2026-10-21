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
	"errors"
	"fmt"
	"slices"

	parser "github.com/haproxytech/client-native/v6/config-parser"
	parsererrors "github.com/haproxytech/client-native/v6/config-parser/errors"
	"github.com/haproxytech/client-native/v6/config-parser/types"
	"github.com/haproxytech/client-native/v6/configuration"
	"github.com/haproxytech/client-native/v6/models"
)

// ConfigSnippet is the config snippet to write with a backend, which the
// client-native models do not carry. When Set is false, the backend keeps the
// snippet it has in the configuration, otherwise its snippet is replaced by
// Lines, removed when they are empty.
type ConfigSnippet struct {
	Lines []string
	Set   bool
}

// BackendConfigSnippet returns the lines of the config snippet of the backend
// name in the configuration of p, nil when it has none.
func BackendConfigSnippet(p parser.Parser, name string) ([]string, error) {
	data, err := p.Get(parser.Backends, name, "config-snippet")
	if err != nil {
		if errors.Is(err, parsererrors.ErrFetch) {
			return nil, nil
		}
		return nil, err
	}
	snippet, ok := data.(*types.StringSliceC)
	if !ok {
		return nil, fmt.Errorf("backend %s: unexpected config snippet type %T", name, data)
	}
	return snippet.Value, nil
}

// setBackendConfigSnippet sets the lines of the config snippet of the backend
// name in the configuration of p, or removes it when there are none.
func setBackendConfigSnippet(p parser.Parser, name string, lines []string) error {
	if len(lines) == 0 {
		return p.Set(parser.Backends, name, "config-snippet", nil)
	}
	return p.Set(parser.Backends, name, "config-snippet", types.StringSliceC{Value: lines})
}

// editBackend rewrites the backend from its model, and writes its config
// snippet, or keeps the one it had when configSnippet is not set, since the
// backend section is deleted and serialized again from the model. It returns
// whether the snippet changed.
func (c *clientNative) editBackend(config configuration.Configuration, backend models.Backend,
	configSnippet ConfigSnippet,
) (bool, error) {
	p, err := config.GetParser(c.activeTransaction)
	if err != nil {
		return false, err
	}
	previous, err := BackendConfigSnippet(p, backend.Name)
	if err != nil {
		return false, err
	}
	if err := config.EditStructuredBackend(backend.Name, &backend, c.activeTransaction, 0); err != nil {
		return false, err
	}
	lines := previous
	if configSnippet.Set {
		lines = configSnippet.Lines
	}
	if len(lines) > 0 {
		p, err = config.GetParser(c.activeTransaction)
		if err != nil {
			return false, err
		}
		if err := setBackendConfigSnippet(p, backend.Name, lines); err != nil {
			return false, err
		}
	}
	return !slices.Equal(previous, lines), nil
}
