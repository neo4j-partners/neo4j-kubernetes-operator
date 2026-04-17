/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package resources_test

// Contract tests for the selector helpers in cluster.go.
//
// These tests exist because label-producer / label-consumer drift has caused
// production incidents (see issue #68). The fix was to centralise the
// selectors; these tests make the centralisation load-bearing by failing
// loudly if anyone changes the label builders without updating the selectors.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// assertSubset fails the test if any key in subset is missing from or has a
// different value in superset. This is the core contract: a selector must be
// a non-strict subset of the labels the builder emits.
func assertSubset(t *testing.T, subset, superset map[string]string, context string) {
	t.Helper()
	for k, v := range subset {
		got, ok := superset[k]
		require.True(t, ok, "%s: selector key %q missing from labels %v", context, k, superset)
		assert.Equal(t, v, got, "%s: selector[%q]=%q but labels[%q]=%q", context, k, v, k, got)
	}
}

// assertNotSubset fails if subset is fully contained in superset. Used to
// assert selectors do NOT accidentally match sibling workloads (e.g. the
// server selector must not match backup pods).
func assertNotSubset(t *testing.T, subset, superset map[string]string, context string) {
	t.Helper()
	for k, v := range subset {
		if got, ok := superset[k]; !ok || got != v {
			return
		}
	}
	t.Errorf("%s: selector %v unexpectedly matches labels %v", context, subset, superset)
}

func newTestCluster() *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}
}

func TestServerPodSelector_MatchesServerPodTemplateLabels(t *testing.T) {
	cluster := newTestCluster()
	sts := resources.BuildServerStatefulSetForEnterprise(cluster)
	require.NotNil(t, sts)

	selector := resources.ServerPodSelector(cluster.Name)
	assertSubset(t, selector, sts.Spec.Template.Labels,
		"ServerPodSelector must match server StatefulSet pod template labels")
}

// Guards the exact bug from #68: a selector that *looked* right but
// appended "-server" to the cluster name and matched nothing.
func TestServerPodSelector_DoesNotMatchWhenClusterNameSuffixed(t *testing.T) {
	cluster := newTestCluster()
	sts := resources.BuildServerStatefulSetForEnterprise(cluster)

	wrong := map[string]string{
		"app.kubernetes.io/instance":  cluster.Name + "-server",
		"app.kubernetes.io/component": "database",
	}
	assertNotSubset(t, wrong, sts.Spec.Template.Labels,
		"historical buggy selector must NOT match server pods (regression guard for #68)")
}

// The backup StatefulSet pods must not be picked up by the server selector,
// since controllers use it to stop/wait for the cluster proper.
func TestServerPodSelector_DoesNotMatchBackupPods(t *testing.T) {
	cluster := newTestCluster()
	cluster.Spec.Backups = &neo4jv1beta1.BackupsSpec{}
	backupSTS := resources.BuildBackupStatefulSet(cluster)
	require.NotNil(t, backupSTS, "backup STS should be built when Backups is set")

	selector := resources.ServerPodSelector(cluster.Name)
	assertNotSubset(t, selector, backupSTS.Spec.Template.Labels,
		"ServerPodSelector must NOT match backup pod labels")
}

func TestPVCSelectorByInstance_MatchesVolumeClaimTemplateLabels(t *testing.T) {
	cluster := newTestCluster()
	sts := resources.BuildServerStatefulSetForEnterprise(cluster)
	require.NotEmpty(t, sts.Spec.VolumeClaimTemplates)

	selector := resources.PVCSelectorByInstance(cluster.Name)
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		assertSubset(t, selector, vct.Labels,
			"PVCSelectorByInstance must match VolumeClaimTemplate "+vct.Name+" labels")
	}
}

func TestPVCSelectorByInstance_MatchesGetLabelsForPVC(t *testing.T) {
	selector := resources.PVCSelectorByInstance("inst-1")
	for _, role := range []string{"server", "data", "backup"} {
		labels := resources.GetLabelsForPVC("inst-1", role)
		assertSubset(t, selector, labels,
			"PVCSelectorByInstance must be a subset of GetLabelsForPVC("+role+")")
	}
}

// StandalonePodSelector is a tiny one-liner. This test still matters: it
// locks in the label key used by the standalone StatefulSet pod template so
// that renaming it anywhere breaks a test, not production.
func TestStandalonePodSelector_UsesAppKey(t *testing.T) {
	selector := resources.StandalonePodSelector("my-standalone")
	require.Len(t, selector, 1)
	assert.Equal(t, "my-standalone", selector["app"])
}
