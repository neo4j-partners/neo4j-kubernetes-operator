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

package controller

import (
	"sort"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Ownership-tracked metadata reconciliation for the annotation and label maps
// the operator manages on owned resources (Services, Ingresses, NetworkPolicies,
// Certificates). This mirrors the env-var ownership protocol
// (cluster-controller-env-vars): the operator records the keys it owns so a
// later reconcile can apply desired values, remove keys it previously owned but
// the spec no longer requests, and never disturb keys written by other actors
// (cert-manager, ingress controllers, cloud load-balancer controllers).
//
// The two owned-key sets are themselves stored as annotations. They are
// operator-internal: they never appear in a desired set and are preserved as
// "foreign" by the merge before being re-stamped, so they don't recurse into
// the managed set.
const (
	ownedAnnotationKeysAnnotation = "neo4j.com/owned-annotation-keys"
	ownedLabelKeysAnnotation      = "neo4j.com/owned-label-keys"
)

// splitOwnedKeys parses a sorted, comma-separated owned-key list.
func splitOwnedKeys(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ownedKeysCSV renders keys as a sorted, comma-separated list for storage.
func ownedKeysCSV(keys []string) string {
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// mergeOwnedStringMap reconciles a controller-managed string map (labels or
// annotations) against the desired set using the ownership model: desired keys
// are added/updated, keys this controller previously owned but no longer
// desires are removed, and keys owned by other actors are preserved. Returns the
// merged map and the new sorted owned-key list. Pure function; the caller
// persists the owned-key list.
func mergeOwnedStringMap(live, desired map[string]string, previousOwned []string) (map[string]string, []string) {
	out := make(map[string]string, len(live)+len(desired))
	for k, v := range live {
		out[k] = v
	}
	desiredKeys := make(map[string]struct{}, len(desired))
	for k := range desired {
		desiredKeys[k] = struct{}{}
	}
	// Remove keys we used to own that are no longer desired. Foreign keys (never
	// owned by us) are left untouched.
	for _, k := range previousOwned {
		if _, stillDesired := desiredKeys[k]; !stillDesired {
			delete(out, k)
		}
	}
	// Apply desired (add / overwrite).
	for k, v := range desired {
		out[k] = v
	}
	owned := make([]string, 0, len(desired))
	for k := range desired {
		owned = append(owned, k)
	}
	sort.Strings(owned)
	return out, owned
}

// applyOwnedMetadata reconciles obj's annotations and labels against the desired
// sets using the ownership model above, tracking owned keys in two bookkeeping
// annotations. Returns true if the annotations or labels changed (nil and empty
// maps are treated as equal so unchanged resources don't churn).
func applyOwnedMetadata(obj client.Object, desiredAnnotations, desiredLabels map[string]string) bool {
	annos := obj.GetAnnotations()
	labels := obj.GetLabels()

	prevOwnedAnnos := splitOwnedKeys(annos[ownedAnnotationKeysAnnotation])
	prevOwnedLabels := splitOwnedKeys(annos[ownedLabelKeysAnnotation])

	newAnnos, ownedAnnos := mergeOwnedStringMap(annos, desiredAnnotations, prevOwnedAnnos)
	newLabels, ownedLabels := mergeOwnedStringMap(labels, desiredLabels, prevOwnedLabels)

	// Persist the owned-key bookkeeping (in annotations for both maps — label
	// values can't hold comma-separated lists safely).
	if len(ownedAnnos) > 0 {
		newAnnos[ownedAnnotationKeysAnnotation] = ownedKeysCSV(ownedAnnos)
	} else {
		delete(newAnnos, ownedAnnotationKeysAnnotation)
	}
	if len(ownedLabels) > 0 {
		newAnnos[ownedLabelKeysAnnotation] = ownedKeysCSV(ownedLabels)
	} else {
		delete(newAnnos, ownedLabelKeysAnnotation)
	}

	changed := false
	if !stringMapsEqual(annos, newAnnos) {
		obj.SetAnnotations(newAnnos)
		changed = true
	}
	if !stringMapsEqual(labels, newLabels) {
		obj.SetLabels(newLabels)
		changed = true
	}
	return changed
}

// stringMapsEqual treats nil and empty maps as equal.
func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
