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
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestValidateMCPConfig(t *testing.T) {
	tests := []struct {
		name           string
		spec           *neo4jv1alpha1.MCPServerSpec
		expectedErrors int
		errorTypes     []field.ErrorType
	}{
		{
			name: "disabled MCP — no errors",
			spec: &neo4jv1alpha1.MCPServerSpec{Enabled: false},
		},
		{
			name: "enabled with no image — uses official default, no error",
			spec: &neo4jv1alpha1.MCPServerSpec{Enabled: true},
		},
		{
			name: "enabled with explicit image — valid",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Image:   validMCPImage(),
			},
		},
		{
			name: "invalid transport",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "grpc",
				Image:     validMCPImage(),
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeNotSupported},
		},
		{
			name: "http auth with secretName — valid override",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Auth: &neo4jv1alpha1.MCPAuthSpec{
					SecretName: "custom-secret",
				},
			},
		},
		{
			name: "auth set without secretName — error",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Auth:    &neo4jv1alpha1.MCPAuthSpec{},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeRequired},
		},
		{
			name: "http port out of range",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				HTTP:      &neo4jv1alpha1.MCPHTTPConfig{Port: 70000},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeInvalid},
		},
		{
			name: "http service port out of range",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				HTTP: &neo4jv1alpha1.MCPHTTPConfig{
					Service: &neo4jv1alpha1.MCPServiceSpec{Port: 70000},
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeInvalid},
		},
		{
			name: "stdio without auth — uses cluster admin secret automatically",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "stdio",
			},
		},
		{
			name: "stdio with auth override — valid",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "stdio",
				Auth: &neo4jv1alpha1.MCPAuthSpec{
					SecretName:  "custom-auth",
					UsernameKey: "user",
					PasswordKey: "pass",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateMCPConfig(tt.spec, field.NewPath("spec", "mcp"))
			assert.Len(t, errs, tt.expectedErrors)
			for _, expectedType := range tt.errorTypes {
				found := false
				for _, err := range errs {
					if err.Type == expectedType {
						found = true
						break
					}
				}
				assert.True(t, found, "expected error type %s not found in %v", expectedType, errs)
			}
		})
	}
}

func validMCPImage() *neo4jv1alpha1.ImageSpec {
	return &neo4jv1alpha1.ImageSpec{
		Repo: "mcp/neo4j-cypher",
		Tag:  "latest",
	}
}
