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

package resources

import (
	"fmt"
	"os"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	mcpContainerName   = "neo4j-mcp"
	mcpHTTPPortDefault = 8000
	mcpHTTPPathDefault = "/mcp/"
	// Official image: https://hub.docker.com/r/mcp/neo4j-cypher
	mcpImageRepoDefault = "mcp/neo4j-cypher"
	mcpImageTagDefault  = "latest"
)

var (
	defaultMCPUID int64 = 65532
)

// BuildMCPDeploymentForCluster builds the MCP Deployment for a cluster.
func BuildMCPDeploymentForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *appsv1.Deployment {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled {
		return nil
	}

	mcp := cluster.Spec.MCP
	image := mcpImage(mcp)
	if image == "" {
		return nil
	}

	replicas := int32(1)
	if mcp.Replicas != nil {
		replicas = *mcp.Replicas
	}

	labels := mcpLabelsForCluster(cluster, mcp)
	secretName, usernameKey, passwordKey := mcpAuthSecretName(cluster.Spec.Auth, mcp)
	env := buildMCPEnv(mcp, mcpNeo4jURIForCluster(cluster), secretName, usernameKey, passwordKey)

	podSecurityContext, containerSecurityContext := mcpSecurityContext(mcp)

	container := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy(mcp),
		Env:             env,
		Resources:       resourceRequirements(mcp),
		SecurityContext: containerSecurityContext,
	}

	if mcpTransport(mcp) == "http" {
		container.Ports = []corev1.ContainerPort{
			{
				Name:          "mcp",
				ContainerPort: mcpHTTPPort(mcp),
				Protocol:      corev1.ProtocolTCP,
			},
		}
	}

	podSpec := corev1.PodSpec{
		SecurityContext:  podSecurityContext,
		Containers:       []corev1.Container{container},
		ImagePullSecrets: imagePullSecrets(mcp),
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-mcp", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: mcpSelectorLabels(cluster.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildMCPDeploymentForStandalone builds the MCP Deployment for a standalone deployment.
func BuildMCPDeploymentForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *appsv1.Deployment {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	mcp := standalone.Spec.MCP
	image := mcpImage(mcp)
	if image == "" {
		return nil
	}

	replicas := int32(1)
	if mcp.Replicas != nil {
		replicas = *mcp.Replicas
	}

	labels := mcpLabelsForStandalone(standalone, mcp)
	secretName, usernameKey, passwordKey := mcpAuthSecretName(standalone.Spec.Auth, mcp)
	env := buildMCPEnv(mcp, mcpNeo4jURIForStandalone(standalone), secretName, usernameKey, passwordKey)

	podSecurityContext, containerSecurityContext := mcpSecurityContext(mcp)

	container := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy(mcp),
		Env:             env,
		Resources:       resourceRequirements(mcp),
		SecurityContext: containerSecurityContext,
	}

	if mcpTransport(mcp) == "http" {
		container.Ports = []corev1.ContainerPort{
			{
				Name:          "mcp",
				ContainerPort: mcpHTTPPort(mcp),
				Protocol:      corev1.ProtocolTCP,
			},
		}
	}

	podSpec := corev1.PodSpec{
		SecurityContext:  podSecurityContext,
		Containers:       []corev1.Container{container},
		ImagePullSecrets: imagePullSecrets(mcp),
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-mcp", standalone.Name),
			Namespace: standalone.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: mcpSelectorLabels(standalone.Name),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildMCPServiceForCluster builds the MCP Service for a cluster.
func BuildMCPServiceForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *corev1.Service {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(cluster.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPService(cluster.Namespace, cluster.Name, mcpLabelsForCluster(cluster, cluster.Spec.MCP), cluster.Spec.MCP)
}

// BuildMCPServiceForStandalone builds the MCP Service for a standalone deployment.
func BuildMCPServiceForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *corev1.Service {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(standalone.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPService(standalone.Namespace, standalone.Name, mcpLabelsForStandalone(standalone, standalone.Spec.MCP), standalone.Spec.MCP)
}

// BuildMCPIngressForCluster builds an MCP Ingress for a cluster.
func BuildMCPIngressForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *networkingv1.Ingress {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(cluster.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPIngress(cluster.Namespace, cluster.Name, mcpLabelsForCluster(cluster, cluster.Spec.MCP), cluster.Spec.MCP)
}

// BuildMCPIngressForStandalone builds an MCP Ingress for a standalone deployment.
func BuildMCPIngressForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *networkingv1.Ingress {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(standalone.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPIngress(standalone.Namespace, standalone.Name, mcpLabelsForStandalone(standalone, standalone.Spec.MCP), standalone.Spec.MCP)
}

// BuildMCPRouteForCluster builds an MCP Route for a cluster.
func BuildMCPRouteForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *unstructured.Unstructured {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(cluster.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPRoute(cluster.Namespace, cluster.Name, mcpLabelsForCluster(cluster, cluster.Spec.MCP), cluster.Spec.MCP)
}

// BuildMCPRouteForStandalone builds an MCP Route for a standalone deployment.
func BuildMCPRouteForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *unstructured.Unstructured {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	if mcpTransport(standalone.Spec.MCP) != "http" {
		return nil
	}

	return buildMCPRoute(standalone.Namespace, standalone.Name, mcpLabelsForStandalone(standalone, standalone.Spec.MCP), standalone.Spec.MCP)
}

func buildMCPService(namespace, name string, labels map[string]string, mcp *neo4jv1alpha1.MCPServerSpec) *corev1.Service {
	serviceType := corev1.ServiceTypeClusterIP
	annotations := map[string]string{}

	if mcp.HTTP != nil && mcp.HTTP.Service != nil {
		if mcp.HTTP.Service.Type != "" {
			serviceType = corev1.ServiceType(mcp.HTTP.Service.Type)
		}
		if mcp.HTTP.Service.Annotations != nil {
			annotations = mcp.HTTP.Service.Annotations
		}
	}

	httpPort := mcpHTTPPort(mcp)
	servicePort := httpPort
	if mcp.HTTP != nil && mcp.HTTP.Service != nil && mcp.HTTP.Service.Port > 0 {
		servicePort = mcp.HTTP.Service.Port
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-mcp", name),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: mcpSelectorLabels(name),
			Ports: []corev1.ServicePort{
				{
					Name:       "mcp",
					Port:       servicePort,
					TargetPort: intstr.FromInt(int(httpPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if mcp.HTTP != nil && mcp.HTTP.Service != nil {
		if mcp.HTTP.Service.LoadBalancerIP != "" {
			svc.Spec.LoadBalancerIP = mcp.HTTP.Service.LoadBalancerIP
		}
		if len(mcp.HTTP.Service.LoadBalancerSourceRanges) > 0 {
			svc.Spec.LoadBalancerSourceRanges = mcp.HTTP.Service.LoadBalancerSourceRanges
		}
		if mcp.HTTP.Service.ExternalTrafficPolicy != "" {
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyType(mcp.HTTP.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

func buildMCPIngress(namespace, name string, labels map[string]string, mcp *neo4jv1alpha1.MCPServerSpec) *networkingv1.Ingress {
	if mcp.HTTP == nil || mcp.HTTP.Service == nil || mcp.HTTP.Service.Ingress == nil || !mcp.HTTP.Service.Ingress.Enabled {
		return nil
	}

	ingressSpec := mcp.HTTP.Service.Ingress
	servicePort := mcpHTTPPort(mcp)
	if mcp.HTTP.Service.Port > 0 {
		servicePort = mcp.HTTP.Service.Port
	}

	mcpPath := mcpHTTPPath(mcp)

	var tls []networkingv1.IngressTLS
	if ingressSpec.TLSSecretName != "" {
		tls = []networkingv1.IngressTLS{
			{
				Hosts:      []string{ingressSpec.Host},
				SecretName: ingressSpec.TLSSecretName,
			},
		}
	}

	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     mcpPath,
			PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypePrefix; return &pt }(),
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: fmt.Sprintf("%s-mcp", name),
					Port: networkingv1.ServiceBackendPort{
						Number: servicePort,
					},
				},
			},
		},
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-mcp-ingress", name),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: ingressSpec.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressSpec.ClassName,
			TLS:              tls,
			Rules: []networkingv1.IngressRule{
				{
					Host: ingressSpec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}
}

func buildMCPRoute(namespace, name string, labels map[string]string, mcp *neo4jv1alpha1.MCPServerSpec) *unstructured.Unstructured {
	if mcp.HTTP == nil || mcp.HTTP.Service == nil || mcp.HTTP.Service.Route == nil || !mcp.HTTP.Service.Route.Enabled {
		return nil
	}

	routeSpec := mcp.HTTP.Service.Route
	annotations := map[string]string{}
	if mcp.HTTP.Service.Annotations != nil {
		for k, v := range mcp.HTTP.Service.Annotations {
			annotations[k] = v
		}
	}
	if routeSpec.Annotations != nil {
		for k, v := range routeSpec.Annotations {
			annotations[k] = v
		}
	}

	servicePort := mcpHTTPPort(mcp)
	if mcp.HTTP.Service.Port > 0 {
		servicePort = mcp.HTTP.Service.Port
	}

	targetPort := routeSpec.TargetPort
	if targetPort == 0 {
		targetPort = servicePort
	}

	path := routeSpec.Path
	if path == "" {
		path = mcpHTTPPath(mcp)
	}

	return buildRoute(
		fmt.Sprintf("%s-mcp-route", name),
		namespace,
		fmt.Sprintf("%s-mcp", name),
		labels,
		annotations,
		routeSpec.Host,
		path,
		targetPort,
		routeSpec.TLS,
	)
}

func mcpLabelsForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, mcp *neo4jv1alpha1.MCPServerSpec) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/version":    mcpImageTag(mcp),
		"app.kubernetes.io/component":  "mcp",
		"app.kubernetes.io/part-of":    "neo4j-cluster",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            cluster.Name,
		"neo4j.com/component":          "mcp",
	}

	return labels
}

func mcpLabelsForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone, mcp *neo4jv1alpha1.MCPServerSpec) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   standalone.Name,
		"app.kubernetes.io/version":    mcpImageTag(mcp),
		"app.kubernetes.io/component":  "mcp",
		"app.kubernetes.io/part-of":    "neo4j-standalone",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            standalone.Name,
		"neo4j.com/component":          "mcp",
	}

	return labels
}

func mcpSelectorLabels(name string) map[string]string {
	return map[string]string{
		"neo4j.com/cluster":   name,
		"neo4j.com/component": "mcp",
	}
}

func mcpImage(spec *neo4jv1alpha1.MCPServerSpec) string {
	repo := mcpImageRepo(spec)
	tag := mcpImageTag(spec)
	if repo == "" || tag == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", repo, tag)
}

func mcpImageTag(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.Image != nil && spec.Image.Tag != "" {
		return spec.Image.Tag
	}
	if operatorVersion := os.Getenv(operatorVersionEnv); operatorVersion != "" {
		return operatorVersion
	}
	return mcpImageTagDefault
}

func mcpImageRepo(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.Image != nil && spec.Image.Repo != "" {
		return spec.Image.Repo
	}
	return mcpImageRepoDefault
}

func mcpTransport(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec == nil || spec.Transport == "" {
		return "http"
	}
	return spec.Transport
}

func mcpHTTPPort(spec *neo4jv1alpha1.MCPServerSpec) int32 {
	if spec != nil && spec.HTTP != nil && spec.HTTP.Port > 0 {
		return spec.HTTP.Port
	}
	return mcpHTTPPortDefault
}

func mcpHTTPHost(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.HTTP != nil && spec.HTTP.Host != "" {
		return spec.HTTP.Host
	}
	return "0.0.0.0"
}

func mcpHTTPPath(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.HTTP != nil && spec.HTTP.Path != "" {
		return spec.HTTP.Path
	}
	return mcpHTTPPathDefault
}

func mcpNeo4jURIForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	serviceName := fmt.Sprintf("%s-client", cluster.Name)
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == CertManagerMode {
		return fmt.Sprintf("neo4j+ssc://%s.%s.svc.cluster.local:7687", serviceName, cluster.Namespace)
	}
	return fmt.Sprintf("neo4j://%s.%s.svc.cluster.local:7687", serviceName, cluster.Namespace)
}

func mcpNeo4jURIForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) string {
	serviceName := fmt.Sprintf("%s-service", standalone.Name)
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == CertManagerMode {
		return fmt.Sprintf("bolt+ssc://%s.%s.svc.cluster.local:7687", serviceName, standalone.Namespace)
	}
	return fmt.Sprintf("bolt://%s.%s.svc.cluster.local:7687", serviceName, standalone.Namespace)
}

// buildMCPEnv constructs the environment variables for the official mcp/neo4j-cypher image.
// Variable naming follows the official image specification:
// https://hub.docker.com/r/mcp/neo4j-cypher
func buildMCPEnv(spec *neo4jv1alpha1.MCPServerSpec, neo4jURI string, secretName, usernameKey, passwordKey string) []corev1.EnvVar {
	// NEO4J_URL and credentials are required by the official image for all transports.
	env := []corev1.EnvVar{
		{Name: "NEO4J_URL", Value: neo4jURI},
		{
			Name: "NEO4J_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  usernameKey,
				},
			},
		},
		{
			Name: "NEO4J_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  passwordKey,
				},
			},
		},
		{Name: "NEO4J_READ_ONLY", Value: strconv.FormatBool(spec.ReadOnly)},
	}

	if spec.Database != "" {
		env = append(env, corev1.EnvVar{Name: "NEO4J_DATABASE", Value: spec.Database})
	}
	if spec.Namespace != "" {
		env = append(env, corev1.EnvVar{Name: "NEO4J_NAMESPACE", Value: spec.Namespace})
	}
	if spec.SchemaSampleSize != nil {
		env = append(env, corev1.EnvVar{Name: "NEO4J_SCHEMA_SAMPLE_SIZE", Value: strconv.Itoa(int(*spec.SchemaSampleSize))})
	}
	if spec.ResponseTokenLimit != nil {
		env = append(env, corev1.EnvVar{Name: "NEO4J_RESPONSE_TOKEN_LIMIT", Value: strconv.Itoa(int(*spec.ResponseTokenLimit))})
	}

	switch mcpTransport(spec) {
	case "http":
		env = append(env,
			corev1.EnvVar{Name: "NEO4J_TRANSPORT", Value: "http"},
			corev1.EnvVar{Name: "NEO4J_MCP_SERVER_HOST", Value: mcpHTTPHost(spec)},
			corev1.EnvVar{Name: "NEO4J_MCP_SERVER_PORT", Value: strconv.Itoa(int(mcpHTTPPort(spec)))},
			corev1.EnvVar{Name: "NEO4J_MCP_SERVER_PATH", Value: mcpHTTPPath(spec)},
		)
		if spec.HTTP != nil {
			if spec.HTTP.AllowedOrigins != "" {
				env = append(env, corev1.EnvVar{Name: "NEO4J_MCP_SERVER_ALLOW_ORIGINS", Value: spec.HTTP.AllowedOrigins})
			}
			if spec.HTTP.AllowedHosts != "" {
				env = append(env, corev1.EnvVar{Name: "NEO4J_MCP_SERVER_ALLOWED_HOSTS", Value: spec.HTTP.AllowedHosts})
			}
			if spec.HTTP.ReadTimeout != nil {
				env = append(env, corev1.EnvVar{Name: "NEO4J_READ_TIMEOUT", Value: strconv.Itoa(int(*spec.HTTP.ReadTimeout))})
			}
		}
	case "stdio":
		env = append(env, corev1.EnvVar{Name: "NEO4J_TRANSPORT", Value: "stdio"})
	}

	env = append(env, filterMCPEnv(spec.Env)...)
	return env
}

func filterMCPEnv(env []corev1.EnvVar) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}

	// These are set by the operator and must not be overridden by spec.env.
	reserved := map[string]struct{}{
		"NEO4J_URL":                      {},
		"NEO4J_USERNAME":                 {},
		"NEO4J_PASSWORD":                 {},
		"NEO4J_DATABASE":                 {},
		"NEO4J_NAMESPACE":                {},
		"NEO4J_READ_ONLY":                {},
		"NEO4J_SCHEMA_SAMPLE_SIZE":       {},
		"NEO4J_RESPONSE_TOKEN_LIMIT":     {},
		"NEO4J_TRANSPORT":                {},
		"NEO4J_MCP_SERVER_HOST":          {},
		"NEO4J_MCP_SERVER_PORT":          {},
		"NEO4J_MCP_SERVER_PATH":          {},
		"NEO4J_MCP_SERVER_ALLOW_ORIGINS": {},
		"NEO4J_MCP_SERVER_ALLOWED_HOSTS": {},
		"NEO4J_READ_TIMEOUT":             {},
	}

	filtered := make([]corev1.EnvVar, 0, len(env))
	for _, e := range env {
		if _, blocked := reserved[e.Name]; blocked {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

func imagePullPolicy(spec *neo4jv1alpha1.MCPServerSpec) corev1.PullPolicy {
	if spec != nil && spec.Image != nil && spec.Image.PullPolicy != "" {
		return corev1.PullPolicy(spec.Image.PullPolicy)
	}
	return corev1.PullIfNotPresent
}

func imagePullSecrets(spec *neo4jv1alpha1.MCPServerSpec) []corev1.LocalObjectReference {
	if spec == nil || spec.Image == nil || len(spec.Image.PullSecrets) == 0 {
		return nil
	}
	secrets := make([]corev1.LocalObjectReference, 0, len(spec.Image.PullSecrets))
	for _, secret := range spec.Image.PullSecrets {
		if secret == "" {
			continue
		}
		secrets = append(secrets, corev1.LocalObjectReference{Name: secret})
	}
	return secrets
}

func resourceRequirements(spec *neo4jv1alpha1.MCPServerSpec) corev1.ResourceRequirements {
	if spec != nil && spec.Resources != nil {
		return *spec.Resources
	}
	return corev1.ResourceRequirements{}
}

func mcpAuthSecretName(auth *neo4jv1alpha1.AuthSpec, mcp *neo4jv1alpha1.MCPServerSpec) (name, usernameKey, passwordKey string) {
	name = DefaultAdminSecret
	if auth != nil && auth.AdminSecret != "" {
		name = auth.AdminSecret
	}

	usernameKey = "username"
	passwordKey = "password"

	if mcp != nil && mcp.Auth != nil {
		if mcp.Auth.SecretName != "" {
			name = mcp.Auth.SecretName
		}
		if mcp.Auth.UsernameKey != "" {
			usernameKey = mcp.Auth.UsernameKey
		}
		if mcp.Auth.PasswordKey != "" {
			passwordKey = mcp.Auth.PasswordKey
		}
	}

	return name, usernameKey, passwordKey
}

func mcpSecurityContext(spec *neo4jv1alpha1.MCPServerSpec) (*corev1.PodSecurityContext, *corev1.SecurityContext) {
	podContext := &corev1.PodSecurityContext{
		RunAsUser:    ptr.To(defaultMCPUID),
		RunAsGroup:   ptr.To(defaultMCPUID),
		FSGroup:      ptr.To(defaultMCPUID),
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	containerContext := &corev1.SecurityContext{
		RunAsUser:                ptr.To(defaultMCPUID),
		RunAsGroup:               ptr.To(defaultMCPUID),
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	if spec != nil && spec.SecurityContext != nil {
		if spec.SecurityContext.PodSecurityContext != nil {
			podContext = spec.SecurityContext.PodSecurityContext
		}
		if spec.SecurityContext.ContainerSecurityContext != nil {
			containerContext = spec.SecurityContext.ContainerSecurityContext
		}
	}

	return podContext, containerContext
}
