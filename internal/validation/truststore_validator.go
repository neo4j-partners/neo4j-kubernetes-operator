/*
Copyright 2026.

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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// reservedMountPaths are paths the operator manages internally. User-supplied
// extra volume mounts that target any of these paths are rejected — they would
// either silently overlay operator-managed content (data loss / boot failure)
// or, in the case of /truststore, conflict with the JKS produced by the
// truststore-init container.
var reservedMountPaths = []string{
	"/data",
	"/logs",
	"/conf",
	"/ssl",
	"/plugins",
	"/truststore",
	"/truststore-ca",
	"/var/lib/neo4j",
	"/var/lib/neo4j/data",
	"/var/lib/neo4j/logs",
	"/var/lib/neo4j/conf",
	"/var/lib/neo4j/plugins",
	"/var/lib/neo4j/certificates",
}

// ValidateTrustedCASecrets validates the spec.trustedCASecrets list. Each
// entry must have a non-empty Name; Names must be unique across the list
// (they double as keytool aliases inside the truststore).
func ValidateTrustedCASecrets(cas []neo4jv1beta1.TrustedCASecret, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	seen := map[string]int{}
	for i, ca := range cas {
		entryPath := path.Index(i)
		if ca.Name == "" {
			errs = append(errs, field.Required(entryPath.Child("name"), "trustedCASecrets[].name is required"))
			continue
		}
		if first, dup := seen[ca.Name]; dup {
			errs = append(errs, field.Invalid(
				entryPath.Child("name"),
				ca.Name,
				fmt.Sprintf("already used at index %d (Secret name doubles as the keytool alias and must be unique)", first),
			))
			continue
		}
		seen[ca.Name] = i
	}
	return errs
}

// ValidateExtraVolumeMounts checks that user-supplied extraVolumeMounts don't
// target operator-managed paths (which would overlay or fight the operator's
// own volume layout).
//
// We do NOT validate that every mount references a volume in extraVolumes —
// users may legitimately reference operator-managed volumes (e.g. mounting
// the data PVC at a second path). Kubernetes itself rejects mounts that
// reference no volume at admission time.
func ValidateExtraVolumeMounts(mounts []corev1.VolumeMount, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	seenPath := map[string]int{}
	for i, m := range mounts {
		entryPath := path.Index(i)
		if m.Name == "" {
			errs = append(errs, field.Required(entryPath.Child("name"), "extraVolumeMounts[].name is required"))
		}
		if m.MountPath == "" {
			errs = append(errs, field.Required(entryPath.Child("mountPath"), "extraVolumeMounts[].mountPath is required"))
			continue
		}
		// Operator-managed paths are off-limits.
		if isReservedMountPath(m.MountPath) {
			errs = append(errs, field.Forbidden(
				entryPath.Child("mountPath"),
				fmt.Sprintf("%q collides with an operator-managed mount path; pick a different path or use spec.config to influence the existing mount", m.MountPath),
			))
			continue
		}
		if first, dup := seenPath[m.MountPath]; dup {
			errs = append(errs, field.Invalid(
				entryPath.Child("mountPath"),
				m.MountPath,
				fmt.Sprintf("already used at index %d", first),
			))
			continue
		}
		seenPath[m.MountPath] = i
	}
	return errs
}

// ValidateExtraVolumes performs basic structural checks on user-supplied
// extraVolumes. Most of the heavy lifting is done by Kubernetes admission;
// here we just enforce that names are unique and non-empty so the resource
// builder can reference them.
func ValidateExtraVolumes(volumes []corev1.Volume, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	seen := map[string]int{}
	for i, v := range volumes {
		entryPath := path.Index(i)
		if v.Name == "" {
			errs = append(errs, field.Required(entryPath.Child("name"), "extraVolumes[].name is required"))
			continue
		}
		if first, dup := seen[v.Name]; dup {
			errs = append(errs, field.Invalid(
				entryPath.Child("name"),
				v.Name,
				fmt.Sprintf("already used at index %d", first),
			))
			continue
		}
		seen[v.Name] = i
	}
	return errs
}

func isReservedMountPath(p string) bool {
	clean := strings.TrimRight(p, "/")
	for _, r := range reservedMountPaths {
		if clean == r {
			return true
		}
	}
	return false
}
