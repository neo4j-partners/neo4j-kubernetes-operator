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

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
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
