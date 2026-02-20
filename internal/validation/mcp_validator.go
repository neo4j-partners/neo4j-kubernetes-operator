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

	// spec.auth is optional for both transports:
	//   - HTTP:  defaults to the cluster/standalone admin secret when omitted.
	//   - STDIO: same default; explicit secretName/keys allow using a separate secret.
	if spec.Auth != nil && spec.Auth.SecretName == "" {
		// If auth is provided it must at least specify the secret name.
		// When auth is nil the operator uses the cluster admin secret automatically.
		allErrs = append(allErrs, field.Required(
			path.Child("auth", "secretName"),
			"secretName is required when auth is set",
		))
	}

	return allErrs
}
