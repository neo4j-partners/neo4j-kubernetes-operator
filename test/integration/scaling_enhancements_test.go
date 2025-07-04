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

package integration

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

var _ = Describe("Scaling Enhancements", func() {
	Context("Memory Validation", func() {
		It("should validate memory configuration against container limits", func() {
			validator := validation.NewMemoryValidator()

			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Config: map[string]string{
						"server.memory.heap.max_size":  "4g", // Too large
						"server.memory.pagecache.size": "2g", // Too large
					},
				},
			}

			errors := validator.Validate(cluster)
			Expect(errors).To(HaveLen(1))
			Expect(errors[0].Error()).To(ContainSubstring("exceeds container memory limit"))
		})

		It("should provide optimization tips", func() {
			validator := validation.NewMemoryValidator()

			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 2,
					},
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			}

			tips := validator.GetOptimizationTips(cluster)
			Expect(tips).NotTo(BeEmpty())
		})
	})

	Context("Integration Tests", func() {
		It("should skip tests requiring full cluster setup", func() {
			// These tests would require actual Kubernetes cluster with operator
			Skip("Integration tests require full cluster setup")
		})
	})

	Context("End-to-End Scaling Test", func() {
		It("should skip e2e tests", func() {
			// These tests would require actual cluster deployment
			Skip("End-to-end tests require full cluster setup with operator running")
		})
	})
})
