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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stretchr/testify/assert"
)

func TestIsTemplateChangeSignificant(t *testing.T) {
	// Set up logger for tests
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx := context.Background()

	reconciler := &Neo4jEnterpriseClusterReconciler{
		Scheme: runtime.NewScheme(),
	}

	// Helper function to create a basic pod template
	createBasicTemplate := func() corev1.PodTemplateSpec {
		return corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": "neo4j",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "neo4j-sa",
				Containers: []corev1.Container{
					{
						Name:  "neo4j",
						Image: "neo4j:5.26.0-enterprise",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1Gi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
						},
						Env: []corev1.EnvVar{
							{Name: "NEO4J_AUTH", Value: "neo4j/admin123"},
							{Name: "NEO4J_EDITION", Value: "enterprise"},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
		}
	}

	t.Run("should skip template updates during cluster formation for identical templates", func(t *testing.T) {
		template := createBasicTemplate()

		// StatefulSet not ready (simulating cluster formation)
		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 1, // Not all replicas ready
			},
		}

		// Identical templates should not trigger update during formation
		significant := reconciler.isTemplateChangeSignificant(ctx, template, template, sts)
		assert.False(t, significant, "Identical templates should not be considered significant during cluster formation")
	})

	t.Run("should allow critical changes during cluster formation", func(t *testing.T) {
		currentTemplate := createBasicTemplate()
		desiredTemplate := createBasicTemplate()

		// Change image (critical change)
		desiredTemplate.Spec.Containers[0].Image = "neo4j:2025.01.0-enterprise"

		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 1, // Not all replicas ready
			},
		}

		significant := reconciler.isTemplateChangeSignificant(ctx, currentTemplate, desiredTemplate, sts)
		assert.True(t, significant, "Image changes should be considered critical during cluster formation")
	})

	t.Run("should allow non-critical changes in stable clusters", func(t *testing.T) {
		currentTemplate := createBasicTemplate()
		desiredTemplate := createBasicTemplate()

		// Change environment variable (non-critical change)
		desiredTemplate.Spec.Containers[0].Env = append(desiredTemplate.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "NEO4J_DEBUG", Value: "true"})

		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 3, // All replicas ready (stable cluster)
			},
		}

		significant := reconciler.isTemplateChangeSignificant(ctx, currentTemplate, desiredTemplate, sts)
		assert.True(t, significant, "Environment changes should be allowed in stable clusters")
	})

	t.Run("should reject non-critical changes during cluster formation", func(t *testing.T) {
		currentTemplate := createBasicTemplate()
		desiredTemplate := createBasicTemplate()

		// Change environment variable (non-critical change)
		desiredTemplate.Spec.Containers[0].Env = append(desiredTemplate.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "NEO4J_DEBUG", Value: "true"})

		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 1, // Not all replicas ready (forming)
			},
		}

		significant := reconciler.isTemplateChangeSignificant(ctx, currentTemplate, desiredTemplate, sts)
		assert.False(t, significant, "Non-critical changes should be rejected during cluster formation")
	})

	t.Run("should detect resource changes as critical", func(t *testing.T) {
		currentTemplate := createBasicTemplate()
		desiredTemplate := createBasicTemplate()

		// Change memory limit (critical change)
		desiredTemplate.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")

		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 1, // Not all replicas ready
			},
		}

		significant := reconciler.isTemplateChangeSignificant(ctx, currentTemplate, desiredTemplate, sts)
		assert.True(t, significant, "Resource changes should be considered critical")
	})

	t.Run("should detect service account changes as critical", func(t *testing.T) {
		currentTemplate := createBasicTemplate()
		desiredTemplate := createBasicTemplate()

		// Change service account (critical for RBAC)
		desiredTemplate.Spec.ServiceAccountName = "different-sa"

		sts := &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 1, // Not all replicas ready
			},
		}

		significant := reconciler.isTemplateChangeSignificant(ctx, currentTemplate, desiredTemplate, sts)
		assert.True(t, significant, "Service account changes should be considered critical")
	})
}

func TestHasCriticalTemplateChanges(t *testing.T) {
	reconciler := &Neo4jEnterpriseClusterReconciler{}

	t.Run("image changes should be critical", func(t *testing.T) {
		current := corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "neo4j", Image: "neo4j:5.26.0-enterprise"},
				},
			},
		}
		desired := corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "neo4j", Image: "neo4j:2025.01.0-enterprise"},
				},
			},
		}

		critical := reconciler.hasCriticalTemplateChanges(current, desired)
		assert.True(t, critical, "Image changes should be critical")
	})

	t.Run("container count changes should be critical", func(t *testing.T) {
		current := corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "neo4j", Image: "neo4j:5.26.0-enterprise"},
				},
			},
		}
		desired := corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "neo4j", Image: "neo4j:5.26.0-enterprise"},
					{Name: "sidecar", Image: "sidecar:latest"},
				},
			},
		}

		critical := reconciler.hasCriticalTemplateChanges(current, desired)
		assert.True(t, critical, "Container count changes should be critical")
	})

	t.Run("identical templates should not be critical", func(t *testing.T) {
		template := corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				ServiceAccountName: "neo4j-sa",
				Containers: []corev1.Container{
					{
						Name:  "neo4j",
						Image: "neo4j:5.26.0-enterprise",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		}

		critical := reconciler.hasCriticalTemplateChanges(template, template)
		assert.False(t, critical, "Identical templates should not be critical")
	})
}

func TestResourcesEqual(t *testing.T) {
	reconciler := &Neo4jEnterpriseClusterReconciler{}

	t.Run("identical resources should be equal", func(t *testing.T) {
		resources := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
				corev1.ResourceCPU:    resource.MustParse("500m"),
			},
		}

		equal := reconciler.resourcesEqual(resources, resources)
		assert.True(t, equal, "Identical resources should be equal")
	})

	t.Run("different memory limits should not be equal", func(t *testing.T) {
		current := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
		desired := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		}

		equal := reconciler.resourcesEqual(current, desired)
		assert.False(t, equal, "Different memory limits should not be equal")
	})

	t.Run("missing vs present resources should not be equal", func(t *testing.T) {
		current := corev1.ResourceRequirements{}
		desired := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}

		equal := reconciler.resourcesEqual(current, desired)
		assert.False(t, equal, "Missing vs present resources should not be equal")
	})
}

// Helper function
func int32Ptr(i int32) *int32 {
	return &i
}
