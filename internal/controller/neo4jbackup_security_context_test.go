/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// newBackupTestReconciler wires a Neo4jBackupReconciler backed by a fake
// client seeded with the given objects.
func newBackupTestReconciler(t *testing.T, objs ...runtime.Object) *Neo4jBackupReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()
	return &Neo4jBackupReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(16),
	}
}

func minimalClusterForBackup(name, namespace string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
	}
}

// assertHardenedPodSecurityContext fails the test if the given PodSpec
// is missing any element of the operator's data-plane hardening contract.
// Mirrors internal/resources/DefaultNeo4j{Pod,Container}SecurityContext.
func assertHardenedPodSecurityContext(t *testing.T, ps *corev1.PodSpec) {
	t.Helper()
	require.NotNil(t, ps.SecurityContext, "PodSecurityContext must be set")
	require.NotNil(t, ps.SecurityContext.RunAsNonRoot, "RunAsNonRoot must be set")
	assert.True(t, *ps.SecurityContext.RunAsNonRoot, "RunAsNonRoot must be true")
	require.NotNil(t, ps.SecurityContext.RunAsUser, "RunAsUser must be set")
	assert.Equal(t, int64(7474), *ps.SecurityContext.RunAsUser, "RunAsUser must be 7474 (neo4j)")
	require.NotNil(t, ps.SecurityContext.SeccompProfile, "SeccompProfile must be set")
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, ps.SecurityContext.SeccompProfile.Type)

	require.NotEmpty(t, ps.Containers, "PodSpec must have at least one container")
	for _, c := range ps.Containers {
		require.NotNil(t, c.SecurityContext, "container %q must have SecurityContext", c.Name)
		require.NotNil(t, c.SecurityContext.RunAsNonRoot, "container %q RunAsNonRoot must be set", c.Name)
		assert.True(t, *c.SecurityContext.RunAsNonRoot)
		require.NotNil(t, c.SecurityContext.AllowPrivilegeEscalation)
		assert.False(t, *c.SecurityContext.AllowPrivilegeEscalation, "AllowPrivilegeEscalation must be false")
		require.NotNil(t, c.SecurityContext.Capabilities, "Capabilities.Drop must be set")
		assert.Contains(t, c.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"),
			"container %q must drop ALL capabilities", c.Name)
	}
}

// TestBackupJobHasHardenedSecurityContext closes the security gap from the
// November 2025 review: the one-shot backup Job had no SecurityContext at
// all. The reviewer's exact concern was that compromised backup pods
// would run as root.
func TestBackupJobHasHardenedSecurityContext(t *testing.T) {
	const ns = "default"
	const clusterName = "test-cluster"
	cluster := minimalClusterForBackup(clusterName, ns)

	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b1", Namespace: ns},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target: neo4jv1beta1.BackupTarget{
				Kind: "Cluster",
				Name: clusterName,
			},
		},
	}
	r := newBackupTestReconciler(t, cluster, backup)
	require.NoError(t, r.Client.Create(context.Background(), &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: backupServiceAccountName, Namespace: ns},
	}))

	job, err := r.createBackupJob(context.Background(), backup, cluster)
	require.NoError(t, err)
	require.NotNil(t, job)

	got := &batchv1.Job{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{Name: job.Name, Namespace: ns}, got))
	assertHardenedPodSecurityContext(t, &got.Spec.Template.Spec)
}

// TestBackupCronJobHasHardenedSecurityContext closes the same gap on the
// scheduled-backup path.
func TestBackupCronJobHasHardenedSecurityContext(t *testing.T) {
	const ns = "default"
	const clusterName = "test-cluster"
	cluster := minimalClusterForBackup(clusterName, ns)

	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b1", Namespace: ns},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target: neo4jv1beta1.BackupTarget{
				Kind: "Cluster",
				Name: clusterName,
			},
			Schedule: "0 2 * * *",
		},
	}
	r := newBackupTestReconciler(t, cluster, backup)
	require.NoError(t, r.Client.Create(context.Background(), &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: backupServiceAccountName, Namespace: ns},
	}))

	cron, err := r.createBackupCronJob(context.Background(), backup, cluster)
	require.NoError(t, err)
	require.NotNil(t, cron)

	got := &batchv1.CronJob{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{Name: cron.Name, Namespace: ns}, got))
	assertHardenedPodSecurityContext(t, &got.Spec.JobTemplate.Spec.Template.Spec)
}
