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

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestConfigValidator_ValidateDiscoveryRestrictions(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name         string
		config       map[string]string
		expectErrors bool
		errorType    string
	}{
		{
			name: "valid configuration without discovery settings",
			config: map[string]string{
				"dbms.logs.query.enabled":  "INFO",
				"dbms.transaction.timeout": "60s",
				"metrics.enabled":          "true",
			},
			expectErrors: false,
		},
		{
			name: "forbidden resolver_type configuration",
			config: map[string]string{
				"dbms.cluster.discovery.resolver_type": "dns",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden static endpoints configuration",
			config: map[string]string{
				"dbms.cluster.discovery.v2.endpoints": "server1:6000,server2:6000",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden legacy endpoints configuration",
			config: map[string]string{
				"dbms.cluster.endpoints": "server1:6000,server2:6000",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden manual kubernetes label selector",
			config: map[string]string{
				"dbms.kubernetes.label_selector": "app=my-app",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "forbidden manual kubernetes service port",
			config: map[string]string{
				"dbms.kubernetes.discovery.service_port_name": "custom-port",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "multiple forbidden discovery settings",
			config: map[string]string{
				"dbms.cluster.discovery.resolver_type": "list",
				"dbms.cluster.discovery.v2.endpoints":  "server1:6000",
				"dbms.kubernetes.label_selector":       "app=test",
			},
			expectErrors: true,
			errorType:    "Forbidden",
		},
		{
			name: "deprecated discovery version setting - invalid value",
			config: map[string]string{
				"dbms.cluster.discovery.version": "V1_ONLY",
			},
			expectErrors: true,
			errorType:    "Unsupported value", // This generates both Invalid (deprecated) and Unsupported (invalid value)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Config: tt.config,
				},
			}

			errors := validator.Validate(cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch: %v", errors)

			if tt.expectErrors && tt.errorType != "" {
				found := false
				errorTypes := []string{}
				for _, err := range errors {
					errorTypes = append(errorTypes, err.Type.String())
					if err.Type.String() == tt.errorType {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected error type %s not found. Available types: %v. Errors: %v", tt.errorType, errorTypes, errors)
			}
		})
	}
}

func TestConfigValidator_ValidDiscoveryVersion(t *testing.T) {
	validator := NewConfigValidator()

	tests := []struct {
		name         string
		version      string
		expectErrors bool
	}{
		{
			name:         "valid V2_ONLY version for Neo4j 5.x",
			version:      "V2_ONLY",
			expectErrors: false, // V2_ONLY is required for Neo4j 5.26+
		},
		{
			name:         "invalid V1_ONLY version",
			version:      "V1_ONLY",
			expectErrors: true,
		},
		{
			name:         "invalid empty version",
			version:      "",
			expectErrors: true,
		},
		{
			name:         "invalid random version",
			version:      "INVALID",
			expectErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Config: map[string]string{
						"dbms.cluster.discovery.version": tt.version,
					},
				},
			}

			errors := validator.Validate(cluster)
			hasErrors := len(errors) > 0
			assert.Equal(t, tt.expectErrors, hasErrors, "Error expectation mismatch for version %s: %v", tt.version, errors)
		})
	}
}
