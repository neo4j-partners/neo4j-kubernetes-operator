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

package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("MCP Integration Tests", func() {
	var (
		ctx          context.Context
		namespace    *corev1.Namespace
		cluster      *neo4jv1alpha1.Neo4jEnterpriseCluster
		standalone   *neo4jv1alpha1.Neo4jEnterpriseStandalone
		curlJob      *batchv1.Job
		clusterName  string
		standaloneID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespaceName := createTestNamespace("mcp")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		clusterName = randomName("mcp-cluster")
		standaloneID = randomName("mcp-standalone")
	})

	AfterEach(func() {
		if cluster != nil {
			By("Cleaning up MCP cluster resource")
			cleanupResource(cluster, namespace.Name, "Neo4jEnterpriseCluster")
			cluster = nil
		}

		if standalone != nil {
			By("Cleaning up MCP standalone resource")
			cleanupResource(standalone, namespace.Name, "Neo4jEnterpriseStandalone")
			standalone = nil
		}

		if curlJob != nil {
			By("Cleaning up MCP curl job")
			_ = k8sClient.Delete(ctx, curlJob)
			curlJob = nil
		}

		cleanupCustomResourcesInNamespace(namespace.Name)

		if namespace != nil {
			By("Deleting MCP test namespace")
			_ = k8sClient.Delete(ctx, namespace)
		}
	})

	Context("MCP HTTP transport", func() {
		It("creates MCP deployment and service for a cluster", func() {
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			cluster = createBasicCluster(clusterName, namespace.Name)
			cluster.Spec.Auth = &neo4jv1alpha1.AuthSpec{
				AdminSecret: adminSecret.Name,
			}
			cluster.Spec.Resources = getCIAppropriateResourceRequirements()
			cluster.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Image: &neo4jv1alpha1.ImageSpec{
					Repo: "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp",
					Tag:  "latest",
				},
				Transport: "http",
				ReadOnly:  true,
				Telemetry: false,
			}

			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			deploymentKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-mcp", clusterName),
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				deployment := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, deploymentKey, deployment); err != nil {
					return err
				}

				container := findContainer(deployment.Spec.Template.Spec.Containers, "neo4j-mcp")
				if container == nil {
					return fmt.Errorf("mcp container not found")
				}

				if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
					return fmt.Errorf("expected 1 MCP replica, got %v", deployment.Spec.Replicas)
				}

				if deployment.Spec.Template.Labels["neo4j.com/component"] != "mcp" {
					return fmt.Errorf("expected MCP label neo4j.com/component=mcp")
				}

				if deployment.Spec.Template.Labels["neo4j.com/cluster"] != clusterName {
					return fmt.Errorf("expected MCP label neo4j.com/cluster=%s", clusterName)
				}

				if container.Image != "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp:latest" {
					return fmt.Errorf("unexpected MCP image: %s", container.Image)
				}

				if !hasContainerPort(container.Ports, 8080) {
					return fmt.Errorf("expected MCP container port 8080")
				}

				transport := findEnvVar(container.Env, "NEO4J_MCP_TRANSPORT")
				if transport == nil || transport.Value != "http" {
					return fmt.Errorf("expected NEO4J_MCP_TRANSPORT=http")
				}

				hostEnv := findEnvVar(container.Env, "NEO4J_MCP_HTTP_HOST")
				if hostEnv == nil || hostEnv.Value != "0.0.0.0" {
					return fmt.Errorf("expected NEO4J_MCP_HTTP_HOST=0.0.0.0")
				}

				portEnv := findEnvVar(container.Env, "NEO4J_MCP_HTTP_PORT")
				if portEnv == nil || portEnv.Value != "8080" {
					return fmt.Errorf("expected NEO4J_MCP_HTTP_PORT=8080")
				}

				neo4jURI := findEnvVar(container.Env, "NEO4J_URI")
				expectedURI := fmt.Sprintf("neo4j://%s-client.%s.svc.cluster.local:7687", clusterName, namespace.Name)
				if neo4jURI == nil || neo4jURI.Value != expectedURI {
					return fmt.Errorf("expected NEO4J_URI=%s", expectedURI)
				}

				readOnly := findEnvVar(container.Env, "NEO4J_READ_ONLY")
				if readOnly == nil || readOnly.Value != "true" {
					return fmt.Errorf("expected NEO4J_READ_ONLY=true")
				}

				telemetry := findEnvVar(container.Env, "NEO4J_TELEMETRY")
				if telemetry == nil || telemetry.Value != "false" {
					return fmt.Errorf("expected NEO4J_TELEMETRY=false")
				}

				if findEnvVar(container.Env, "NEO4J_USERNAME") != nil {
					return fmt.Errorf("NEO4J_USERNAME should not be set for HTTP transport")
				}

				if findEnvVar(container.Env, "NEO4J_MCP_HTTP_TLS_ENABLED") != nil {
					return fmt.Errorf("NEO4J_MCP_HTTP_TLS_ENABLED should not be set without TLS")
				}

				return nil
			}, timeout, interval).Should(Succeed())

			serviceKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-mcp", clusterName),
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				service := &corev1.Service{}
				if err := k8sClient.Get(ctx, serviceKey, service); err != nil {
					return err
				}

				if service.Spec.Selector["neo4j.com/component"] != "mcp" {
					return fmt.Errorf("expected service selector neo4j.com/component=mcp")
				}

				if service.Spec.Selector["neo4j.com/cluster"] != clusterName {
					return fmt.Errorf("expected service selector neo4j.com/cluster=%s", clusterName)
				}

				if len(service.Spec.Ports) == 0 || service.Spec.Ports[0].Port != 8080 {
					return fmt.Errorf("expected MCP service port 8080")
				}

				return nil
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("MCP HTTP runtime verification", func() {
		It("serves tools/list over HTTP with Basic Auth", func() {
			imageSpec := mcpRuntimeImageSpec()
			pullSecretName := createRegistryPullSecret(ctx, namespace.Name)

			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			standalone = createBasicStandalone(standaloneID, namespace.Name)
			standalone.Spec.Auth = &neo4jv1alpha1.AuthSpec{
				AdminSecret: adminSecret.Name,
			}
			standalone.Spec.Resources = getCIAppropriateResourceRequirements()
			standalone.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Image: &neo4jv1alpha1.ImageSpec{
					Repo:        imageSpec.Repo,
					Tag:         imageSpec.Tag,
					PullSecrets: []string{pullSecretName},
				},
				Transport: "http",
				ReadOnly:  true,
				Telemetry: false,
			}

			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			Eventually(func() bool {
				var currentStandalone neo4jv1alpha1.Neo4jEnterpriseStandalone
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      standaloneID,
					Namespace: namespace.Name,
				}, &currentStandalone)
				if err != nil {
					return false
				}
				return currentStandalone.Status.Phase == "Ready"
			}, clusterTimeout, interval).Should(BeTrue())

			deploymentKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-mcp", standaloneID),
				Namespace: namespace.Name,
			}

			Eventually(func() bool {
				deployment := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, deploymentKey, deployment); err != nil {
					return false
				}
				return deployment.Status.ReadyReplicas > 0
			}, timeout, interval).Should(BeTrue())

			serviceHost := fmt.Sprintf("%s-mcp.%s.svc.cluster.local", standaloneID, namespace.Name)
			serviceURL := fmt.Sprintf("http://%s:8080/mcp", serviceHost)

			curlJob = buildMCPCurlJob(namespace.Name, "mcp-tools-list", serviceURL, "neo4j", "admin123")
			Expect(k8sClient.Create(ctx, curlJob)).To(Succeed())

			Eventually(func() error {
				job := &batchv1.Job{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: curlJob.Name, Namespace: namespace.Name}, job); err != nil {
					return err
				}
				if job.Status.Failed > 0 {
					dumpJobLogs(namespace.Name, job.Name)
					return fmt.Errorf("mcp curl job failed")
				}
				if job.Status.Succeeded > 0 {
					return nil
				}
				return fmt.Errorf("mcp curl job not completed yet")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("MCP STDIO transport", func() {
		It("creates MCP deployment without service for standalone", func() {
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: namespace.Name,
				},
				StringData: map[string]string{
					"username": "neo4j",
					"password": "admin123",
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			standalone = createBasicStandalone(standaloneID, namespace.Name)
			standalone.Spec.Auth = &neo4jv1alpha1.AuthSpec{
				AdminSecret: adminSecret.Name,
			}
			standalone.Spec.Resources = getCIAppropriateResourceRequirements()
			standalone.Spec.MCP = &neo4jv1alpha1.MCPServerSpec{
				Enabled: true,
				Image: &neo4jv1alpha1.ImageSpec{
					Repo: "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp",
					Tag:  "latest",
				},
				Transport: "stdio",
				ReadOnly:  true,
				Telemetry: false,
				Auth: &neo4jv1alpha1.MCPAuthSpec{
					SecretName:  adminSecret.Name,
					UsernameKey: "username",
					PasswordKey: "password",
				},
			}

			Expect(k8sClient.Create(ctx, standalone)).To(Succeed())

			deploymentKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-mcp", standaloneID),
				Namespace: namespace.Name,
			}

			Eventually(func() error {
				deployment := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, deploymentKey, deployment); err != nil {
					return err
				}

				container := findContainer(deployment.Spec.Template.Spec.Containers, "neo4j-mcp")
				if container == nil {
					return fmt.Errorf("mcp container not found")
				}

				if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
					return fmt.Errorf("expected 1 MCP replica, got %v", deployment.Spec.Replicas)
				}

				if deployment.Spec.Template.Labels["neo4j.com/component"] != "mcp" {
					return fmt.Errorf("expected MCP label neo4j.com/component=mcp")
				}

				if deployment.Spec.Template.Labels["neo4j.com/cluster"] != standaloneID {
					return fmt.Errorf("expected MCP label neo4j.com/cluster=%s", standaloneID)
				}

				if container.Image != "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp:latest" {
					return fmt.Errorf("unexpected MCP image: %s", container.Image)
				}

				transport := findEnvVar(container.Env, "NEO4J_MCP_TRANSPORT")
				if transport == nil || transport.Value != "stdio" {
					return fmt.Errorf("expected NEO4J_MCP_TRANSPORT=stdio")
				}

				neo4jURI := findEnvVar(container.Env, "NEO4J_URI")
				expectedURI := fmt.Sprintf("bolt://%s-service.%s.svc.cluster.local:7687", standaloneID, namespace.Name)
				if neo4jURI == nil || neo4jURI.Value != expectedURI {
					return fmt.Errorf("expected NEO4J_URI=%s", expectedURI)
				}

				readOnly := findEnvVar(container.Env, "NEO4J_READ_ONLY")
				if readOnly == nil || readOnly.Value != "true" {
					return fmt.Errorf("expected NEO4J_READ_ONLY=true")
				}

				telemetry := findEnvVar(container.Env, "NEO4J_TELEMETRY")
				if telemetry == nil || telemetry.Value != "false" {
					return fmt.Errorf("expected NEO4J_TELEMETRY=false")
				}

				username := findEnvVar(container.Env, "NEO4J_USERNAME")
				if username == nil || username.ValueFrom == nil || username.ValueFrom.SecretKeyRef == nil {
					return fmt.Errorf("expected NEO4J_USERNAME secret ref")
				}

				if username.ValueFrom.SecretKeyRef.Name != adminSecret.Name || username.ValueFrom.SecretKeyRef.Key != "username" {
					return fmt.Errorf("unexpected NEO4J_USERNAME secret ref")
				}

				password := findEnvVar(container.Env, "NEO4J_PASSWORD")
				if password == nil || password.ValueFrom == nil || password.ValueFrom.SecretKeyRef == nil {
					return fmt.Errorf("expected NEO4J_PASSWORD secret ref")
				}

				if password.ValueFrom.SecretKeyRef.Name != adminSecret.Name || password.ValueFrom.SecretKeyRef.Key != "password" {
					return fmt.Errorf("unexpected NEO4J_PASSWORD secret ref")
				}

				if findEnvVar(container.Env, "NEO4J_MCP_HTTP_PORT") != nil {
					return fmt.Errorf("NEO4J_MCP_HTTP_PORT should not be set for STDIO")
				}

				if len(container.Ports) != 0 {
					return fmt.Errorf("expected no MCP container ports for STDIO")
				}

				return nil
			}, timeout, interval).Should(Succeed())

			serviceKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-mcp", standaloneID),
				Namespace: namespace.Name,
			}

			Consistently(func() bool {
				service := &corev1.Service{}
				err := k8sClient.Get(ctx, serviceKey, service)
				return errors.IsNotFound(err)
			}, 20*time.Second, 2*time.Second).Should(BeTrue())
		})
	})
})

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func findEnvVar(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

func hasContainerPort(ports []corev1.ContainerPort, port int32) bool {
	for _, p := range ports {
		if p.ContainerPort == port {
			return true
		}
	}
	return false
}

func buildMCPCurlJob(namespace, name, url, username, password string) *batchv1.Job {
	script := strings.Join([]string{
		"response=\"\"",
		"i=0",
		"while [ $i -lt 30 ]; do",
		fmt.Sprintf("response=$(curl -sS -u %s:%s -H \"Content-Type: application/json\" -d '{\"jsonrpc\":\"2.0\",\"method\":\"tools/list\",\"id\":1}' %s || true)", username, password, url),
		"echo \"$response\"",
		"echo \"$response\" | grep -q '\"tools\"' && exit 0",
		"i=$((i+1))",
		"sleep 5",
		"done",
		"exit 1",
	}, "; ")

	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", name, time.Now().Unix()),
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "curl",
							Image:   "curlimages/curl:8.6.0",
							Command: []string{"/bin/sh", "-c", script},
						},
					},
				},
			},
		},
	}
}

func dumpJobLogs(namespace, jobName string) {
	podList := &corev1.PodList{}
	if err := k8sClient.List(ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{"job-name": jobName}); err != nil {
		GinkgoWriter.Printf("Failed to list job pods: %v\n", err)
		return
	}

	if len(podList.Items) == 0 {
		GinkgoWriter.Printf("No pods found for job %s\n", jobName)
		return
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		GinkgoWriter.Printf("Failed to create clientset for logs: %v\n", err)
		return
	}

	var tailLines int64 = 200
	for _, pod := range podList.Items {
		req := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			TailLines: &tailLines,
		})
		data, err := req.Do(ctx).Raw()
		if err != nil {
			GinkgoWriter.Printf("Failed to get logs for %s: %v\n", pod.Name, err)
			continue
		}
		if len(data) == 0 {
			GinkgoWriter.Printf("Logs for %s: <empty>\n", pod.Name)
			continue
		}
		GinkgoWriter.Printf("Logs for %s:\n%s\n", pod.Name, string(data))
	}
}

func mcpRuntimeImageSpec() *neo4jv1alpha1.ImageSpec {
	image := os.Getenv("MCP_TEST_IMAGE")
	if image == "" {
		return &neo4jv1alpha1.ImageSpec{
			Repo: "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp",
			Tag:  "latest",
		}
	}

	repo, tag := splitImageTag(image)
	return &neo4jv1alpha1.ImageSpec{
		Repo: repo,
		Tag:  tag,
	}
}

func splitImageTag(image string) (string, string) {
	colon := strings.LastIndex(image, ":")
	if colon == -1 || colon < strings.LastIndex(image, "/") {
		return image, "latest"
	}
	return image[:colon], image[colon+1:]
}

func createRegistryPullSecret(ctx context.Context, namespace string) string {
	registry := os.Getenv("MCP_REGISTRY_SERVER")
	if registry == "" {
		registry = "ghcr.io"
	}

	username := os.Getenv("MCP_REGISTRY_USERNAME")
	password := os.Getenv("MCP_REGISTRY_PASSWORD")
	if username == "" {
		username = os.Getenv("GITHUB_ACTOR")
	}
	if password == "" {
		password = os.Getenv("GITHUB_TOKEN")
	}

	if username == "" || password == "" {
		Fail("registry credentials missing: set MCP_REGISTRY_USERNAME and MCP_REGISTRY_PASSWORD (or GITHUB_ACTOR/GITHUB_TOKEN)")
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
	dockerConfig := map[string]map[string]map[string]string{
		"auths": {
			registry: {
				"username": username,
				"password": password,
				"auth":     auth,
			},
		},
	}
	configJSON, err := json.Marshal(dockerConfig)
	if err != nil {
		Fail(fmt.Sprintf("failed to build dockerconfigjson: %v", err))
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-registry-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: configJSON,
		},
	}
	if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
		Fail(fmt.Sprintf("failed to create registry pull secret: %v", err))
	}

	return secret.Name
}
