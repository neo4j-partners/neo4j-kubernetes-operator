/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
)

// Plugin Controller Tests are included in the main Controller Suite

var _ = Describe("Neo4jPlugin Controller", func() {
	// Note: Deployment detection tests are in plugin_controller_unit_test.go (internal methods)

	// Note: Helper function unit tests are in plugin_controller_unit_test.go

	Context("Plugin Installation", func() {
		// Note: Plugin installation tests require access to unexported methods
		// These tests will be added when the plugin controller methods are made public
		// or when using the real controller with integration tests
	})
})

// Unit tests for unexported methods are in plugin_controller_unit_test.go
