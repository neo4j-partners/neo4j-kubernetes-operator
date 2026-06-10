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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMergeOwnedStringMap(t *testing.T) {
	t.Run("adds and updates desired, preserves foreign", func(t *testing.T) {
		live := map[string]string{"foreign": "x", "a": "old"}
		out, owned := mergeOwnedStringMap(live, map[string]string{"a": "new", "b": "1"}, nil)
		assert.Equal(t, "new", out["a"])
		assert.Equal(t, "1", out["b"])
		assert.Equal(t, "x", out["foreign"], "foreign key preserved")
		assert.Equal(t, []string{"a", "b"}, owned)
	})

	t.Run("removes previously-owned key no longer desired, keeps foreign", func(t *testing.T) {
		live := map[string]string{"a": "1", "b": "2", "foreign": "keep"}
		out, owned := mergeOwnedStringMap(live, map[string]string{"a": "1"}, []string{"a", "b"})
		assert.Equal(t, "1", out["a"])
		_, hasB := out["b"]
		assert.False(t, hasB, "previously-owned b dropped from spec is removed")
		assert.Equal(t, "keep", out["foreign"], "foreign key untouched")
		assert.Equal(t, []string{"a"}, owned)
	})

	t.Run("a key removed from spec that was NOT owned stays (foreign)", func(t *testing.T) {
		live := map[string]string{"b": "2"}
		out, _ := mergeOwnedStringMap(live, map[string]string{}, nil)
		assert.Equal(t, "2", out["b"], "never-owned key is foreign and preserved")
	})
}

// TestApplyOwnedMetadata_LifecycleAcrossReconciles models the Service/Ingress
// reconcile across operations and pins the three Bugbot findings:
//   - foreign annotations (cert-manager / ingress controller) are preserved and
//     don't cause oscillation (#1);
//   - desired labels are applied (#2);
//   - a key cleared from the spec is removed (#3).
func TestApplyOwnedMetadata_LifecycleAcrossReconciles(t *testing.T) {
	// First reconcile: empty live object, operator wants annotation X + label L.
	obj := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc"}}
	changed := applyOwnedMetadata(obj, map[string]string{"X": "1"}, map[string]string{"L": "a"})
	assert.True(t, changed, "first apply sets desired + bookkeeping")
	assert.Equal(t, "1", obj.Annotations["X"])
	assert.Equal(t, "a", obj.Labels["L"])
	assert.Equal(t, "X", obj.Annotations[ownedAnnotationKeysAnnotation])
	assert.Equal(t, "L", obj.Annotations[ownedLabelKeysAnnotation])

	// A foreign controller adds its own annotation + label to the live object.
	obj.Annotations["cert-manager.io/issuer"] = "ca"
	obj.Labels["foreign"] = "z"

	// Steady-state reconcile with the SAME desired set must not change anything
	// (no oscillation, #1) and must preserve the foreign keys.
	changed = applyOwnedMetadata(obj, map[string]string{"X": "1"}, map[string]string{"L": "a"})
	assert.False(t, changed, "unchanged desired must not churn")
	assert.Equal(t, "ca", obj.Annotations["cert-manager.io/issuer"], "foreign annotation preserved")
	assert.Equal(t, "z", obj.Labels["foreign"], "foreign label preserved")

	// Spec changes: X's value updated, new annotation Y added, label L removed.
	changed = applyOwnedMetadata(obj, map[string]string{"X": "2", "Y": "9"}, map[string]string{})
	assert.True(t, changed)
	assert.Equal(t, "2", obj.Annotations["X"], "updated value applied")
	assert.Equal(t, "9", obj.Annotations["Y"], "new desired annotation applied")
	assert.Equal(t, "ca", obj.Annotations["cert-manager.io/issuer"], "foreign still preserved")
	_, hasL := obj.Labels["L"]
	assert.False(t, hasL, "label cleared from spec is removed (#3)")
	assert.Equal(t, "z", obj.Labels["foreign"], "foreign label still preserved")
	assert.Equal(t, "X,Y", obj.Annotations[ownedAnnotationKeysAnnotation])
	_, hasLabelTracking := obj.Annotations[ownedLabelKeysAnnotation]
	assert.False(t, hasLabelTracking, "owned-label bookkeeping dropped when no labels owned")
}

// TestApplyOwnedMetadata_NoManagedKeysNoChurn ensures an object the operator
// manages no annotations/labels for is left untouched (no bookkeeping added).
func TestApplyOwnedMetadata_NoManagedKeysNoChurn(t *testing.T) {
	obj := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc"}}
	assert.False(t, applyOwnedMetadata(obj, nil, nil), "no desired keys => no change")
	assert.Empty(t, obj.Annotations)
	assert.Empty(t, obj.Labels)
}
