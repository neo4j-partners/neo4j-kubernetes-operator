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
			name: "disabled MCP",
			spec: &neo4jv1alpha1.MCPServerSpec{Enabled: false},
		},
		{
			name: "missing image",
			spec: &neo4jv1alpha1.MCPServerSpec{Enabled: true},
		},
		{
			name: "missing image repo and tag",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Image:   &neo4jv1alpha1.ImageSpec{},
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
			name: "http with auth set",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Image:     validMCPImage(),
				Auth: &neo4jv1alpha1.MCPAuthSpec{
					SecretName: "auth-secret",
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeInvalid},
		},
		{
			name: "http port out of range",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Image:     validMCPImage(),
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
				Image:     validMCPImage(),
				HTTP: &neo4jv1alpha1.MCPHTTPConfig{
					Service: &neo4jv1alpha1.MCPServiceSpec{Port: 70000},
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeInvalid},
		},
		{
			name: "http tls secret missing secretName",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Image:     validMCPImage(),
				HTTP: &neo4jv1alpha1.MCPHTTPConfig{
					TLS: &neo4jv1alpha1.MCPTLSSpec{Mode: "secret"},
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeRequired},
		},
		{
			name: "http tls disabled with secretName",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Image:     validMCPImage(),
				HTTP: &neo4jv1alpha1.MCPHTTPConfig{
					TLS: &neo4jv1alpha1.MCPTLSSpec{
						Mode:       "disabled",
						SecretName: "mcp-tls",
					},
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeInvalid},
		},
		{
			name: "http tls cert-manager missing issuerRef name",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "http",
				Image:     validMCPImage(),
				HTTP: &neo4jv1alpha1.MCPHTTPConfig{
					TLS: &neo4jv1alpha1.MCPTLSSpec{
						Mode:      "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{},
					},
				},
			},
			expectedErrors: 1,
			errorTypes:     []field.ErrorType{field.ErrorTypeRequired},
		},
		{
			name: "stdio auth missing fields",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "stdio",
				Image:     validMCPImage(),
				Auth:      &neo4jv1alpha1.MCPAuthSpec{},
			},
			expectedErrors: 3,
			errorTypes:     []field.ErrorType{field.ErrorTypeRequired},
		},
		{
			name: "stdio without auth",
			spec: &neo4jv1alpha1.MCPServerSpec{
				Enabled:   true,
				Transport: "stdio",
				Image:     validMCPImage(),
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
		Repo: "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp",
		Tag:  "v1.0.0",
	}
}
