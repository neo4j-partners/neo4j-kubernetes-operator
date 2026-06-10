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
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// makeUpgradeScheme registers the required API groups into a runtime.Scheme.
func makeUpgradeScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

// clusterForUpgrade returns a minimal Neo4jEnterpriseCluster suitable for upgrade tests.
func clusterForUpgrade(name, ns, currentTag, targetTag string, servers int32) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{
				Repo: "neo4j",
				Tag:  targetTag,
			},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: servers},
			Storage:  neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "1Gi"},
		},
		Status: neo4jv1beta1.Neo4jEnterpriseClusterStatus{
			Version: currentTag,
		},
	}
}

// serverSTSForUpgrade returns a minimal StatefulSet for the server pods.
func serverSTSForUpgrade(clusterName, ns, image string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-server", clusterName),
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": clusterName},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type:          appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "neo4j",
						Image: image,
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestInitializeUpgradeStatus
// ---------------------------------------------------------------------------

func TestInitializeUpgradeStatus(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("mycluster", "default", "5.26.0-enterprise", "2025.01.0-enterprise", 3)

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()

	orch := &RollingUpgradeOrchestrator{Client: fc}

	if err := orch.initializeUpgradeStatus(context.Background(), cluster); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cluster.Status.UpgradeStatus == nil {
		t.Fatal("UpgradeStatus should be set after initialization")
	}
	if cluster.Status.UpgradeStatus.Phase != "InProgress" {
		t.Errorf("expected Phase=InProgress, got %q", cluster.Status.UpgradeStatus.Phase)
	}
	if cluster.Status.UpgradeStatus.PreviousVersion != "5.26.0-enterprise" {
		t.Errorf("expected PreviousVersion=5.26.0-enterprise, got %q", cluster.Status.UpgradeStatus.PreviousVersion)
	}
	if cluster.Status.UpgradeStatus.TargetVersion != "2025.01.0-enterprise" {
		t.Errorf("expected TargetVersion=2025.01.0-enterprise, got %q", cluster.Status.UpgradeStatus.TargetVersion)
	}
	if cluster.Status.UpgradeStatus.Progress == nil {
		t.Fatal("Progress should not be nil")
	}
	if cluster.Status.UpgradeStatus.Progress.Total != 3 {
		t.Errorf("expected Total=3, got %d", cluster.Status.UpgradeStatus.Progress.Total)
	}
	if cluster.Status.UpgradeStatus.Progress.Pending != 3 {
		t.Errorf("expected Pending=3, got %d", cluster.Status.UpgradeStatus.Progress.Pending)
	}
}

// ---------------------------------------------------------------------------
// TestGetServerStatefulSet_Found / NotFound
// ---------------------------------------------------------------------------

func TestGetServerStatefulSet_Found(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("demo", "default", "", "5.26.0-enterprise", 2)
	sts := serverSTSForUpgrade("demo", "default", "neo4j:5.26.0-enterprise", 2)

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	orch := &RollingUpgradeOrchestrator{Client: fc}
	got, err := orch.getServerStatefulSet(context.Background(), cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "demo-server" {
		t.Errorf("expected name 'demo-server', got %q", got.Name)
	}
}

func TestGetServerStatefulSet_NotFound(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("ghost", "default", "", "5.26.0-enterprise", 2)

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()

	orch := &RollingUpgradeOrchestrator{Client: fc}
	_, err := orch.getServerStatefulSet(context.Background(), cluster)
	if err == nil {
		t.Fatal("expected error when StatefulSet not found")
	}
}

// ---------------------------------------------------------------------------
// TestUpdateServerStatefulSet_AppliesMutation
// ---------------------------------------------------------------------------

func TestUpdateServerStatefulSet_AppliesMutation(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("mycluster", "default", "", "2025.01.0-enterprise", 3)
	sts := serverSTSForUpgrade("mycluster", "default", "neo4j:5.26.0-enterprise", 3)

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	orch := &RollingUpgradeOrchestrator{Client: fc}

	newImage := "neo4j:2025.01.0-enterprise"
	updated, err := orch.updateServerStatefulSet(context.Background(), cluster, func(s *appsv1.StatefulSet) {
		s.Spec.Template.Spec.Containers[0].Image = newImage
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Spec.Template.Spec.Containers[0].Image != newImage {
		t.Errorf("expected image %q, got %q", newImage, updated.Spec.Template.Spec.Containers[0].Image)
	}

	// Confirm the change was persisted via the fake store
	persisted := &appsv1.StatefulSet{}
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "mycluster-server", Namespace: "default"}, persisted); err != nil {
		t.Fatalf("failed to fetch persisted STS: %v", err)
	}
	if persisted.Spec.Template.Spec.Containers[0].Image != newImage {
		t.Errorf("mutation not persisted: expected %q, got %q", newImage, persisted.Spec.Template.Spec.Containers[0].Image)
	}
}

// ---------------------------------------------------------------------------
// TestUpgradeServers_InitialStagingImage
// Verifies that updateServerStatefulSet, when called with the same mutation
// that upgradeServers uses to stage the initial image + partition freeze,
// produces the expected state.
// ---------------------------------------------------------------------------

func TestUpgradeServers_InitialStagingImage(t *testing.T) {
	scheme := makeUpgradeScheme()
	const replicas int32 = 3
	cluster := clusterForUpgrade("mycluster", "default", "neo4j:5.26.0-enterprise", "2025.01.0-enterprise", replicas)
	sts := serverSTSForUpgrade("mycluster", "default", "neo4j:5.26.0-enterprise", replicas)

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	orch := &RollingUpgradeOrchestrator{Client: fc}

	newImage := "neo4j:2025.01.0-enterprise"
	updated, err := orch.updateServerStatefulSet(context.Background(), cluster, func(s *appsv1.StatefulSet) {
		s.Spec.Template.Spec.Containers[0].Image = newImage
		if s.Spec.Template.Annotations == nil {
			s.Spec.Template.Annotations = make(map[string]string)
		}
		s.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] = time.Now().Format(time.RFC3339)

		if s.Spec.UpdateStrategy.RollingUpdate == nil {
			s.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
		}
		partition := replicas
		s.Spec.UpdateStrategy.RollingUpdate.Partition = &partition
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// (a) image is updated
	if got := updated.Spec.Template.Spec.Containers[0].Image; got != newImage {
		t.Errorf("expected image %q, got %q", newImage, got)
	}
	// (b) initial partition freeze is set to replicas
	if updated.Spec.UpdateStrategy.RollingUpdate == nil || updated.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
		t.Fatal("RollingUpdate.Partition should be set")
	}
	if *updated.Spec.UpdateStrategy.RollingUpdate.Partition != replicas {
		t.Errorf("expected partition=%d, got %d", replicas, *updated.Spec.UpdateStrategy.RollingUpdate.Partition)
	}
	// (c) timestamp annotation is stamped
	if updated.Spec.Template.Annotations["neo4j.com/upgrade-timestamp"] == "" {
		t.Error("expected upgrade-timestamp annotation to be set")
	}
}
