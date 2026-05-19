/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// newPluginTestReconciler builds a Neo4jPluginReconciler backed by a fake
// client seeded with the given objects.
func newPluginTestReconciler(t *testing.T, objs ...runtime.Object) *Neo4jPluginReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()
	return &Neo4jPluginReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(16),
	}
}

// pluginTestSTS returns a minimal cluster StatefulSet shaped like what the
// cluster controller emits — name "<cluster>-server", a single "neo4j"
// container, no pre-existing NEO4J_PLUGINS env var.
func pluginTestSTS(clusterName, namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-server",
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "neo4j"},
					},
				},
			},
		},
	}
}

// findEnv returns (value, true) if name is present on the container, else "", false.
func findEnv(c *corev1.Container, name string) (string, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// TestInstallPluginViaEnvironment_Managed asserts the default-mode behaviour
// is unchanged: NEO4J_PLUGINS is added AND security env vars are written.
func TestInstallPluginViaEnvironment_Managed(t *testing.T) {
	const ns = "default"
	const clusterName = "managed-c"

	sts := pluginTestSTS(clusterName, ns)
	r := newPluginTestReconciler(t, sts)

	plugin := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "gds", Namespace: ns},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			ClusterRef: clusterName,
			Name:       "graph-data-science",
			Version:    "2.13.0",
			// InstallMode left empty — defaults to "Managed".
		},
	}
	deployment := &DeploymentInfo{Type: "cluster", Name: clusterName, Namespace: ns}

	require.NoError(t, r.installPluginViaEnvironment(context.Background(), plugin, deployment))

	got := &appsv1.StatefulSet{}
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: clusterName + "-server", Namespace: ns}, got))
	c := &got.Spec.Template.Spec.Containers[0]

	pluginsVal, hasPlugins := findEnv(c, "NEO4J_PLUGINS")
	require.True(t, hasPlugins, "Managed mode must add NEO4J_PLUGINS")
	assert.Contains(t, pluginsVal, "graph-data-science")

	// GDS triggers automatic security settings — verify one landed.
	unrestricted, ok := findEnv(c, "NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED")
	require.True(t, ok, "Managed mode must write the GDS unrestricted security env var")
	assert.Contains(t, unrestricted, "gds.*")
}

// TestInstallPluginViaEnvironment_PreBaked asserts that PreBaked skips the
// NEO4J_PLUGINS mutation but still writes the plugin's security configuration
// — the whole point of the mode.
func TestInstallPluginViaEnvironment_PreBaked(t *testing.T) {
	const ns = "default"
	const clusterName = "prebaked-c"

	sts := pluginTestSTS(clusterName, ns)
	r := newPluginTestReconciler(t, sts)

	plugin := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "gds", Namespace: ns},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			ClusterRef:  clusterName,
			Name:        "graph-data-science",
			Version:     "2.13.0",
			InstallMode: PluginInstallModePreBaked,
		},
	}
	deployment := &DeploymentInfo{Type: "cluster", Name: clusterName, Namespace: ns}

	require.NoError(t, r.installPluginViaEnvironment(context.Background(), plugin, deployment))

	got := &appsv1.StatefulSet{}
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: clusterName + "-server", Namespace: ns}, got))
	c := &got.Spec.Template.Spec.Containers[0]

	_, hasPlugins := findEnv(c, "NEO4J_PLUGINS")
	assert.False(t, hasPlugins, "PreBaked mode must NOT add NEO4J_PLUGINS")

	// Configuration path must still run — security env vars are why a user
	// keeps the CR around when they bake the JAR into the image.
	unrestricted, ok := findEnv(c, "NEO4J_DBMS_SECURITY_PROCEDURES_UNRESTRICTED")
	require.True(t, ok, "PreBaked mode must still write the GDS unrestricted security env var")
	assert.Contains(t, unrestricted, "gds.*")
}

// TestInstallPluginViaEnvironment_PreBaked_PreservesExistingPluginsEnv asserts
// that if NEO4J_PLUGINS is already present (e.g. set by the user on the
// cluster CR for APOC core), PreBaked does not mutate it.
func TestInstallPluginViaEnvironment_PreBaked_PreservesExistingPluginsEnv(t *testing.T) {
	const ns = "default"
	const clusterName = "prebaked-preserve-c"

	sts := pluginTestSTS(clusterName, ns)
	sts.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "NEO4J_PLUGINS", Value: `["apoc"]`},
	}
	r := newPluginTestReconciler(t, sts)

	plugin := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "gds", Namespace: ns},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			ClusterRef:  clusterName,
			Name:        "graph-data-science",
			Version:     "2.13.0",
			InstallMode: PluginInstallModePreBaked,
		},
	}
	deployment := &DeploymentInfo{Type: "cluster", Name: clusterName, Namespace: ns}

	require.NoError(t, r.installPluginViaEnvironment(context.Background(), plugin, deployment))

	got := &appsv1.StatefulSet{}
	require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: clusterName + "-server", Namespace: ns}, got))

	pluginsVal, _ := findEnv(&got.Spec.Template.Spec.Containers[0], "NEO4J_PLUGINS")
	assert.Equal(t, `["apoc"]`, pluginsVal, "PreBaked must not append GDS to a pre-existing NEO4J_PLUGINS")
}

// TestCheckForDuplicatePlugin_NoConflict — single CR, no other plugins
// in the namespace. Returns "" (no contention).
func TestCheckForDuplicatePlugin_NoConflict(t *testing.T) {
	const ns = "default"
	plugin := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: ns, UID: "uid-1"},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			ClusterRef: "c1",
			Name:       "apoc",
			Version:    "5.26.0",
		},
	}
	r := newPluginTestReconciler(t, plugin)

	winner, err := r.checkForDuplicatePlugin(context.Background(), plugin)
	require.NoError(t, err)
	assert.Equal(t, "", winner, "no other CRs contesting -> empty winner string")
}

// TestCheckForDuplicatePlugin_OlderWins — two CRs for same
// (clusterRef, name). The older one wins; the younger sees the older's
// metadata.name returned and is expected to mark itself Failed.
func TestCheckForDuplicatePlugin_OlderWins(t *testing.T) {
	const ns = "default"
	older := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "older",
			Namespace:         ns,
			UID:               "uid-older",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	newer := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "newer",
			Namespace:         ns,
			UID:               "uid-newer",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	r := newPluginTestReconciler(t, older, newer)

	// From the newer's perspective: older wins.
	winner, err := r.checkForDuplicatePlugin(context.Background(), newer)
	require.NoError(t, err)
	assert.Equal(t, "older", winner)

	// From the older's perspective: it wins itself.
	winner, err = r.checkForDuplicatePlugin(context.Background(), older)
	require.NoError(t, err)
	assert.Equal(t, "older", winner)
}

// TestCheckForDuplicatePlugin_IgnoresDifferentCluster — same plugin
// name but different clusterRef is fine. Two clusters in the same
// namespace can both install APOC; their CRs are independent.
func TestCheckForDuplicatePlugin_IgnoresDifferentCluster(t *testing.T) {
	const ns = "default"
	a := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "apoc-on-a", Namespace: ns, UID: "uid-a"},
		Spec:       neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "cluster-a", Name: "apoc", Version: "5.26.0"},
	}
	b := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "apoc-on-b", Namespace: ns, UID: "uid-b"},
		Spec:       neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "cluster-b", Name: "apoc", Version: "5.26.0"},
	}
	r := newPluginTestReconciler(t, a, b)

	winner, err := r.checkForDuplicatePlugin(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, "", winner, "different clusterRef -> no contention")
}

// TestCheckForDuplicatePlugin_IgnoresDifferentPluginName — same
// cluster but different plugin name is fine. A cluster can install
// both APOC and GDS via two separate Neo4jPlugin CRs.
func TestCheckForDuplicatePlugin_IgnoresDifferentPluginName(t *testing.T) {
	const ns = "default"
	apoc := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "apoc-cr", Namespace: ns, UID: "uid-apoc"},
		Spec:       neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	gds := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "gds-cr", Namespace: ns, UID: "uid-gds"},
		Spec:       neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "graph-data-science", Version: "2.13.0"},
	}
	r := newPluginTestReconciler(t, apoc, gds)

	winner, err := r.checkForDuplicatePlugin(context.Background(), apoc)
	require.NoError(t, err)
	assert.Equal(t, "", winner, "different plugin name -> no contention")
}

// TestCheckForDuplicatePlugin_IgnoresDeletingCR — a duplicate that's
// already mid-delete (DeletionTimestamp set) should not block the
// surviving CR from taking ownership. Otherwise a stuck finalizer on
// the older CR would lock out its replacement indefinitely.
func TestCheckForDuplicatePlugin_IgnoresDeletingCR(t *testing.T) {
	const ns = "default"
	now := metav1.Now()
	older := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "older-deleting",
			Namespace:         ns,
			UID:               "uid-old",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			DeletionTimestamp: &now,
			Finalizers:        []string{"neo4j.neo4j.com/plugin-finalizer"},
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	newer := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "newer-live",
			Namespace:         ns,
			UID:               "uid-new",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	r := newPluginTestReconciler(t, older, newer)

	winner, err := r.checkForDuplicatePlugin(context.Background(), newer)
	require.NoError(t, err)
	assert.Equal(t, "", winner, "older being deleted -> newer takes over with no contention")
}

// TestCheckForDuplicatePlugin_UIDTiebreaker — when two CRs share the
// exact same CreationTimestamp (fast test loops, kubectl apply
// races), the lower UID wins. Deterministic across operator replicas.
func TestCheckForDuplicatePlugin_UIDTiebreaker(t *testing.T) {
	const ns = "default"
	ts := metav1.NewTime(time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	a := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: "a", Namespace: ns, UID: "uid-aaaa",
			CreationTimestamp: ts,
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	b := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: "b", Namespace: ns, UID: "uid-bbbb",
			CreationTimestamp: ts,
		},
		Spec: neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "apoc", Version: "5.26.0"},
	}
	r := newPluginTestReconciler(t, a, b)

	winner, err := r.checkForDuplicatePlugin(context.Background(), b)
	require.NoError(t, err)
	assert.Equal(t, "a", winner, "lower UID wins the tiebreaker")
}
