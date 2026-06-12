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
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = networkingv1.AddToScheme(s)
	return s
}

func minimalCluster(name, ns string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 2},
			Storage:  neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "1Gi"},
		},
	}
}

func configMapWithData(name, ns string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       data,
	}
}

func int32PtrCM(v int32) *int32 { return &v }

func serverSTS(clusterName, ns string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-server",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32PtrCM(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "neo4j", Image: "neo4j:5.26.0-enterprise"}},
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": clusterName},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestCalculateConfigMapHash
// ---------------------------------------------------------------------------

func TestCalculateConfigMapHash(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	cm1 := configMapWithData("a", "ns", map[string]string{
		"neo4j.conf": "server.bolt.enabled=true\n",
	})
	cm2 := configMapWithData("b", "ns", map[string]string{
		"neo4j.conf": "server.bolt.enabled=true\n",
	})
	cm3 := configMapWithData("c", "ns", map[string]string{
		"neo4j.conf": "server.bolt.enabled=false\n",
	})

	h1 := cm.calculateConfigMapHash(cm1)
	h2 := cm.calculateConfigMapHash(cm2)
	h3 := cm.calculateConfigMapHash(cm3)

	if h1 != h2 {
		t.Errorf("identical ConfigMaps must produce the same hash: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different ConfigMaps must produce different hashes")
	}
	// Hash is deterministic across two calls on the same object
	if cm.calculateConfigMapHash(cm1) != h1 {
		t.Errorf("hash must be deterministic")
	}
}

func TestCalculateConfigMapHash_MultipleKeys(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	// Key order in the map must not affect the hash (we hash over a fixed key order)
	a := configMapWithData("a", "ns", map[string]string{
		"neo4j.conf": "key=val\n",
		"startup.sh": "#!/bin/bash\n",
	})
	b := configMapWithData("b", "ns", map[string]string{
		"startup.sh": "#!/bin/bash\n",
		"neo4j.conf": "key=val\n",
	})
	if cm.calculateConfigMapHash(a) != cm.calculateConfigMapHash(b) {
		t.Error("hash should be order-independent because keys are iterated in fixed order")
	}
}

// ---------------------------------------------------------------------------
// TestNormalizeNeo4jConf
// ---------------------------------------------------------------------------

func TestNormalizeNeo4jConf_DeduplicatesKeys(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	input := "key=first\nkey=second\n"
	output := cm.normalizeNeo4jConf(input)

	if output == input {
		// Should have removed the second occurrence
		t.Error("normalizer should remove duplicate keys")
	}
	// Only the first occurrence should remain
	count := 0
	for _, line := range splitLines(output) {
		if line == "key=first" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'key=first' line, got %d", count)
	}
}

func TestNormalizeNeo4jConf_PreservesComments(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	input := "# This is a comment\nkey=value\n"
	output := cm.normalizeNeo4jConf(input)

	if !containsLine(output, "# This is a comment") {
		t.Error("normalizer should preserve comments")
	}
}

func TestNormalizeNeo4jConf_EmptyInput(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())
	out := cm.normalizeNeo4jConf("")
	if out != "" {
		t.Errorf("empty input should produce empty output, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// TestNormalizeStartupScript
// ---------------------------------------------------------------------------

func TestNormalizeStartupScript_ReplacesRuntimeVars(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	input := "#!/bin/bash\nexport POD_ORDINAL=0\nexport HOSTNAME=pod-0\necho $(date)\nstatic line\n"
	output := cm.normalizeStartupScript(input)

	if containsLine(output, "export POD_ORDINAL=0") {
		t.Error("POD_ORDINAL line should be replaced")
	}
	if containsLine(output, "export HOSTNAME=pod-0") {
		t.Error("HOSTNAME line should be replaced")
	}
	if !containsLine(output, "static line") {
		t.Error("static lines should be preserved")
	}
	if !containsLine(output, "# Runtime variable excluded from hash") {
		t.Error("replaced lines should contain placeholder comment")
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeConfigChanges
// ---------------------------------------------------------------------------

func TestAnalyzeConfigChanges(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	old := configMapWithData("old", "ns", map[string]string{
		"neo4j.conf": "removed.key=yes\nshared.key=old\n",
	})
	newCM := configMapWithData("new", "ns", map[string]string{
		"neo4j.conf": "added.key=yes\nshared.key=new\n",
	})

	changes := cm.analyzeConfigChanges(old, newCM)

	found := func(substr string) bool {
		for _, c := range changes {
			if containsSubstr(c, substr) {
				return true
			}
		}
		return false
	}

	if !found("modified neo4j.conf") && !found("modified") {
		t.Errorf("expected 'modified neo4j.conf' in changes, got %v", changes)
	}

	// No changes scenario
	same := cm.analyzeConfigChanges(old, old)
	if len(same) != 1 || !containsSubstr(same[0], "no semantic differences") {
		t.Errorf("identical ConfigMaps should report 'no semantic differences', got %v", same)
	}
}

// ---------------------------------------------------------------------------
// TestRequiresRestart
// ---------------------------------------------------------------------------

func TestRequiresRestart(t *testing.T) {
	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	if !cm.requiresRestart([]string{"modified neo4j.conf"}) {
		t.Error("property change should require restart")
	}
	if cm.requiresRestart([]string{"hash changed but no semantic differences detected"}) {
		t.Error("no-semantic-diff should not require restart")
	}
	if cm.requiresRestart([]string{}) {
		t.Error("empty change list should not require restart")
	}
}

// ---------------------------------------------------------------------------
// TestTriggerRollingRestartForConfigChange — regression guard for Issue #19
// ---------------------------------------------------------------------------

func TestTriggerRollingRestartForConfigChange_StampsAnnotation(t *testing.T) {
	scheme := newTestScheme()
	cluster := minimalCluster("mycluster", "default")
	sts := serverSTS("mycluster", "default")

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	cm := NewConfigMapManager(fc)
	err := cm.triggerRollingRestartForConfigChange(context.Background(), cluster, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify annotation was stamped on the correct StatefulSet
	updated := &appsv1.StatefulSet{}
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "mycluster-server", Namespace: "default"}, updated); err != nil {
		t.Fatalf("failed to fetch updated STS: %v", err)
	}
	if ann := updated.Spec.Template.Annotations["neo4j.neo4j.com/config-hash"]; ann != "abc123" {
		t.Errorf("expected config-hash annotation 'abc123', got %q", ann)
	}
	if updated.Spec.Template.Annotations["neo4j.neo4j.com/config-restart"] == "" {
		t.Error("expected config-restart timestamp annotation to be set")
	}
}

func TestTriggerRollingRestartForConfigChange_MissingSTS(t *testing.T) {
	// No StatefulSet in the fake store — should return nil gracefully
	fc := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	cluster := minimalCluster("missing-cluster", "default")
	cm := NewConfigMapManager(fc)
	if err := cm.triggerRollingRestartForConfigChange(context.Background(), cluster, "hash"); err != nil {
		t.Errorf("expected nil when STS not found, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestHasMemoryConfigChanged
// ---------------------------------------------------------------------------

func TestHasMemoryConfigChanged(t *testing.T) {
	base := minimalCluster("c", "ns")

	other := minimalCluster("c", "ns")
	other.Spec.Config = map[string]string{
		"server.memory.heap.max_size": "2g",
	}

	cm := NewConfigMapManager(fake.NewClientBuilder().WithScheme(newTestScheme()).Build())

	if !cm.HasMemoryConfigChanged(base, other) {
		t.Error("expected memory change detected when heap config differs")
	}
	if cm.HasMemoryConfigChanged(base, base) {
		t.Error("expected no memory change when clusters are identical")
	}

	// Resource limit change
	withLimit := minimalCluster("c", "ns")
	withLimit.Spec.Resources = &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
	}
	if !cm.HasMemoryConfigChanged(base, withLimit) {
		t.Error("expected memory change detected when resource limits differ")
	}
}

// ---------------------------------------------------------------------------
// TestReconcileConfigMap_Creates
// ---------------------------------------------------------------------------

func TestReconcileConfigMap_Creates(t *testing.T) {
	scheme := newTestScheme()
	cluster := minimalCluster("newcluster", "default")
	sts := serverSTS("newcluster", "default")

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	cm := NewConfigMapManager(fc)
	err := cm.ReconcileConfigMap(context.Background(), cluster)
	if err != nil {
		t.Fatalf("ReconcileConfigMap returned error: %v", err)
	}

	// ConfigMap should now exist
	created := &corev1.ConfigMap{}
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "newcluster-config", Namespace: "default"}, created); err != nil {
		t.Errorf("expected ConfigMap to be created, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Small string helpers (avoid importing strings in test without adding to prod)
// ---------------------------------------------------------------------------

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
