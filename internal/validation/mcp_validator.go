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
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

func validateMCPConfig(spec *neo4jv1alpha1.MCPServerSpec, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if spec == nil || !spec.Enabled {
		return allErrs
	}

	// Image is optional; defaults are applied when omitted.

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

	if transport == "http" {
		if spec.Auth != nil {
			allErrs = append(allErrs, field.Invalid(
				path.Child("auth"),
				"<set>",
				"auth is only supported for stdio transport",
			))
		}

		if spec.HTTP != nil {
			if spec.HTTP.Port < 0 || spec.HTTP.Port > 65535 {
				allErrs = append(allErrs, field.Invalid(
					path.Child("http", "port"),
					spec.HTTP.Port,
					"port must be between 1 and 65535 when set",
				))
			}

			if spec.HTTP.TLS != nil {
				tlsPath := path.Child("http", "tls")
				mode := spec.HTTP.TLS.Mode
				if mode == "" {
					mode = "disabled"
				}

				switch mode {
				case "disabled":
					if spec.HTTP.TLS.SecretName != "" {
						allErrs = append(allErrs, field.Invalid(
							tlsPath.Child("secretName"),
							spec.HTTP.TLS.SecretName,
							"secretName should not be set when TLS is disabled",
						))
					}
				case "secret":
					if spec.HTTP.TLS.SecretName == "" {
						allErrs = append(allErrs, field.Required(
							tlsPath.Child("secretName"),
							"secretName is required for secret TLS mode",
						))
					}
				case "cert-manager":
					if spec.HTTP.TLS.IssuerRef == nil || spec.HTTP.TLS.IssuerRef.Name == "" {
						allErrs = append(allErrs, field.Required(
							tlsPath.Child("issuerRef", "name"),
							"issuerRef.name is required for cert-manager TLS mode",
						))
					}
				default:
					allErrs = append(allErrs, field.NotSupported(
						tlsPath.Child("mode"),
						spec.HTTP.TLS.Mode,
						[]string{"disabled", "secret", "cert-manager"},
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
	}

	if transport == "stdio" && spec.Auth != nil {
		if spec.Auth.SecretName == "" {
			allErrs = append(allErrs, field.Required(
				path.Child("auth", "secretName"),
				"secretName is required for stdio transport",
			))
		}
		if spec.Auth.UsernameKey == "" {
			allErrs = append(allErrs, field.Required(
				path.Child("auth", "usernameKey"),
				"usernameKey is required for stdio transport",
			))
		}
		if spec.Auth.PasswordKey == "" {
			allErrs = append(allErrs, field.Required(
				path.Child("auth", "passwordKey"),
				"passwordKey is required for stdio transport",
			))
		}
	}

	return allErrs
}
