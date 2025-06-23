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

package eks

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var (
	cfg           *rest.Config
	k8sClient     client.Client
	clientset     *kubernetes.Clientset
	ctx           context.Context
	cancel        context.CancelFunc
	testNamespace string
)

const (
	timeout  = time.Minute * 10 // Longer timeout for cloud tests
	interval = time.Second * 10
)

func TestEKS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EKS Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	By("Setting up EKS test environment")

	// Skip if not running on EKS
	if os.Getenv("CLUSTER_TYPE") != "eks" {
		Skip("Skipping EKS tests - CLUSTER_TYPE != eks")
	}

	// Use in-cluster config or kubeconfig
	var err error
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	Expect(err).NotTo(HaveOccurred())

	// Create clients
	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())

	// Create test namespace
	testNamespace = fmt.Sprintf("eks-test-%d", time.Now().Unix())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("Cleaning up EKS test environment")

	if testNamespace != "" {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		// Clean up test namespace - errors in cleanup are logged but don't fail the test
		if err := k8sClient.Delete(ctx, ns); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "warning: failed to delete test namespace during cleanup: %v\n", err)
		}
	}

	cancel()
})

// EKS-specific test utilities
func createEKSCluster(name string) *neo4jv1alpha1.Neo4jEnterpriseCluster {
	return &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries:   3,
				Secondaries: 2,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "gp3", // EKS default storage class
				Size:      "20Gi",
			},
			// EKS-specific configurations
			Backups: &neo4jv1alpha1.BackupsSpec{
				DefaultStorage: &neo4jv1alpha1.StorageLocation{
					Type:   "s3",
					Bucket: os.Getenv("S3_BACKUP_BUCKET"),
					Path:   "/eks-backups",
				},
				Cloud: &neo4jv1alpha1.CloudBlock{
					Provider: "aws",
					Identity: &neo4jv1alpha1.CloudIdentity{
						Provider: "aws",
						AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
							Enabled: true,
							Annotations: map[string]string{
								"eks.amazonaws.com/role-arn": os.Getenv("AWS_BACKUP_ROLE_ARN"),
							},
						},
					},
				},
			},
		},
	}
}

func waitForNodeReadiness() {
	Eventually(func() bool {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}

		readyNodes := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyNodes++
					break
				}
			}
		}

		return readyNodes >= 2 // Minimum nodes for testing
	}, timeout, interval).Should(BeTrue())
}

func verifyEKSSpecificFeatures(clusterName string) {
	By("Verifying EKS-specific features")

	// Check if service account has IRSA annotations
	sa := &corev1.ServiceAccount{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterName + "-backup",
			Namespace: testNamespace,
		}, sa)
	}, timeout, interval).Should(Succeed())

	// Verify IRSA annotation
	Expect(sa.Annotations).To(HaveKey("eks.amazonaws.com/role-arn"))

	By("Verifying EBS CSI driver integration")
	// Check if storage class exists and is EBS-based
	// This would be done by checking PVC creation and binding
}
