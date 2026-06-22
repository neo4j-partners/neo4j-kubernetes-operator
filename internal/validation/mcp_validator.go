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

package validation

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func validateMCPConfig(spec *neo4jv1beta1.MCPServerSpec, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if spec == nil || !spec.Enabled {
		return allErrs
	}

	transport := spec.Transport
	if transport == "" {
		transport = "http"
	}
	if transport != "http" && transport != "stdio" {
		allErrs = append(allErrs, field.NotSupported(
			path.Child("transport"),
			spec.Transport,
			[]string{"http", "stdio"},
		))
	}

	if transport == "http" && spec.HTTP != nil {
		if spec.HTTP.Port < 0 || spec.HTTP.Port > 65535 {
			allErrs = append(allErrs, field.Invalid(
				path.Child("http", "port"),
				spec.HTTP.Port,
				"port must be between 1 and 65535 when set",
			))
		}

		// Validate TLS configuration for HTTP transport.
		if spec.HTTP.TLS != nil {
			if spec.HTTP.TLS.SecretName == "" {
				allErrs = append(allErrs, field.Required(
					path.Child("http", "tls", "secretName"),
					"secretName is required when tls is configured",
				))
			}
		}

		if spec.HTTP.Service != nil {
			if spec.HTTP.Service.Port < 0 || spec.HTTP.Service.Port > 65535 {
				allErrs = append(allErrs, field.Invalid(
					path.Child("http", "service", "port"),
					spec.HTTP.Service.Port,
					"port must be between 1 and 65535 when set",
				))
			}
		}
	}

	// spec.auth applies to STDIO transport only.
	// In HTTP mode credentials come per-request from the client's Authorization header
	// (Basic Auth or Bearer token); the operator does not inject credentials for HTTP.
	// Providing auth with HTTP transport is allowed but has no effect; we emit a warning
	// by validating only that secretName is present when auth is set.
	if spec.Auth != nil && spec.Auth.SecretName == "" {
		allErrs = append(allErrs, field.Required(
			path.Child("auth", "secretName"),
			"secretName is required when auth is set",
		))
	}

	return allErrs
}
