/*
Copyright 2025 Priyo Lahiri.

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

package testutil

import "time"

// Common test constants to avoid hardcoded values in tests
const (
	// Neo4j configuration
	TestNeo4jVersion        = "5.26-enterprise"
	TestNeo4jUpgradeVersion = "2025.01.0-enterprise"
	TestNeo4jImage          = "neo4j"

	// Storage configuration
	TestStorageClass     = "standard"
	TestStorageSize      = "10Gi"
	TestSmallStorageSize = "1Gi"

	// Resource configuration
	TestMemoryLimit   = "2Gi"
	TestMemoryRequest = "1Gi"
	TestCPULimit      = "1000m"
	TestCPURequest    = "500m"

	// Test timeouts and intervals
	TestTimeout   = 5 * time.Minute
	TestInterval  = 10 * time.Second
	ShortTimeout  = 30 * time.Second
	ShortInterval = 5 * time.Second

	// Test credentials
	TestPassword = "testpassword123"
	TestUsername = "neo4j"

	// Test topology
	DefaultPrimaries   = 1
	DefaultSecondaries = 0
	TestPrimaries      = 3
	TestSecondaries    = 2

	// Scaling configuration
	MinReplicas   = 1
	MaxReplicas   = 7
	ScaleUpTarget = 20
)
