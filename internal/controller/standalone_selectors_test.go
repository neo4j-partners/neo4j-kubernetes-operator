/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Contract tests for selectors used against Neo4jEnterpriseStandalone
// workloads. Paired with internal/resources/cluster_selectors_test.go, which
// covers the cluster side. Exists as a separate white-box test because the
// standalone StatefulSet is built inside this package (createStatefulSet is
// unexported).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

func TestStandalonePodSelector_MatchesStatefulSetPodTemplate(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	r := &Neo4jEnterpriseStandaloneReconciler{}
	sts := r.createStatefulSet(context.Background(), standalone)
	require.NotNil(t, sts)

	selector := resources.StandalonePodSelector(standalone.Name)
	for k, v := range selector {
		got, ok := sts.Spec.Template.Labels[k]
		require.True(t, ok, "selector key %q missing from pod template labels %v", k, sts.Spec.Template.Labels)
		assert.Equal(t, v, got, "selector[%q] must match pod template label", k)
	}

	// StatefulSet .Spec.Selector and pod template must also agree — if they
	// don't, Kubernetes rejects the STS at create time. Worth asserting here
	// so a future label rename fails in unit tests, not cluster tests.
	for k, v := range sts.Spec.Selector.MatchLabels {
		got, ok := sts.Spec.Template.Labels[k]
		require.True(t, ok)
		assert.Equal(t, v, got, "Selector.MatchLabels[%q] must match pod template label", k)
	}
}

// Guards the (now-fixed) cleanupPVCs regression: the standalone PVC cleanup
// was selecting on "app=<name>", which is a pod label, never a PVC label.
func TestPVCSelectorByInstance_MatchesStandaloneVolumeClaimTemplate(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	r := &Neo4jEnterpriseStandaloneReconciler{}
	sts := r.createStatefulSet(context.Background(), standalone)
	require.NotEmpty(t, sts.Spec.VolumeClaimTemplates)

	selector := resources.PVCSelectorByInstance(standalone.Name)
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		for k, v := range selector {
			got, ok := vct.Labels[k]
			require.True(t, ok, "selector key %q missing from VCT %q labels %v", k, vct.Name, vct.Labels)
			assert.Equal(t, v, got, "VCT %q selector[%q] mismatch", vct.Name, k)
		}
	}

	// Assert the historical buggy selector does NOT match — the one that
	// used the pod label "app=<name>" against PVCs.
	brokenSelector := map[string]string{"app": standalone.Name}
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		_, ok := vct.Labels["app"]
		assert.False(t, ok,
			"VCT %q must not carry pod label %q; cleanupPVCs previously used this selector and matched nothing",
			vct.Name, "app")
		_ = brokenSelector
	}
}
