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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

const (
	mcpContainerName      = "neo4j-mcp"
	mcpHTTPPortDefault    = 8080
	mcpHTTPPortTLSDefault = 8443
	mcpTLSVolumeName      = "mcp-tls"
	mcpTLSMountPath       = "/tls"
	mcpTLSCertFile        = "/tls/tls.crt"
	mcpTLSKeyFile         = "/tls/tls.key"
	mcpImageRepoDefault   = "ghcr.io/neo4j-partners/neo4j-kubernetes-operator-mcp"
	mcpImageTagDefault    = "latest"
	mcpOperatorVersionEnv = "OPERATOR_VERSION"
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
	volumes, volumeMounts := mcpTLSVolume(mcp, cluster.Name)

	podSecurityContext, containerSecurityContext := mcpSecurityContext(mcp)

	container := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy(mcp),
		Env:             env,
		Resources:       resourceRequirements(mcp),
		SecurityContext: containerSecurityContext,
		VolumeMounts:    volumeMounts,
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
		Volumes:          volumes,
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
	volumes, volumeMounts := mcpTLSVolume(mcp, standalone.Name)

	podSecurityContext, containerSecurityContext := mcpSecurityContext(mcp)

	container := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy(mcp),
		Env:             env,
		Resources:       resourceRequirements(mcp),
		SecurityContext: containerSecurityContext,
		VolumeMounts:    volumeMounts,
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
		Volumes:          volumes,
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

// BuildMCPCertificateForCluster builds an MCP TLS Certificate for a cluster.
func BuildMCPCertificateForCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) *certv1.Certificate {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled || cluster.Spec.MCP.HTTP == nil || cluster.Spec.MCP.HTTP.TLS == nil {
		return nil
	}
	if mcpTransport(cluster.Spec.MCP) != "http" {
		return nil
	}
	if cluster.Spec.MCP.HTTP.TLS.Mode != CertManagerMode {
		return nil
	}

	return buildMCPCertificate(cluster.Name, cluster.Namespace, mcpLabelsForCluster(cluster, cluster.Spec.MCP), cluster.Spec.MCP)
}

// BuildMCPCertificateForStandalone builds an MCP TLS Certificate for a standalone deployment.
func BuildMCPCertificateForStandalone(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *certv1.Certificate {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled || standalone.Spec.MCP.HTTP == nil || standalone.Spec.MCP.HTTP.TLS == nil {
		return nil
	}
	if mcpTransport(standalone.Spec.MCP) != "http" {
		return nil
	}
	if standalone.Spec.MCP.HTTP.TLS.Mode != CertManagerMode {
		return nil
	}

	return buildMCPCertificate(standalone.Name, standalone.Namespace, mcpLabelsForStandalone(standalone, standalone.Spec.MCP), standalone.Spec.MCP)
}

func buildMCPCertificate(name, namespace string, labels map[string]string, mcp *neo4jv1alpha1.MCPServerSpec) *certv1.Certificate {
	serviceName := fmt.Sprintf("%s-mcp", name)
	secretName := mcpTLSSecretName(mcp, name)
	if mcp.HTTP.TLS.IssuerRef == nil {
		return nil
	}

	dnsNames := []string{
		serviceName,
		fmt.Sprintf("%s.%s", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	}

	certSpec := certv1.CertificateSpec{
		SecretName: secretName,
		IssuerRef: cmmeta.ObjectReference{
			Name: mcp.HTTP.TLS.IssuerRef.Name,
			Kind: mcp.HTTP.TLS.IssuerRef.Kind,
		},
		CommonName: fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
		DNSNames:   dnsNames,
	}

	if mcp.HTTP.TLS.IssuerRef.Group != "" {
		certSpec.IssuerRef.Group = mcp.HTTP.TLS.IssuerRef.Group
	}

	if mcp.HTTP.TLS.Duration != nil {
		if duration, err := time.ParseDuration(*mcp.HTTP.TLS.Duration); err == nil {
			certSpec.Duration = &metav1.Duration{Duration: duration}
		}
	}

	if mcp.HTTP.TLS.RenewBefore != nil {
		if renewBefore, err := time.ParseDuration(*mcp.HTTP.TLS.RenewBefore); err == nil {
			certSpec.RenewBefore = &metav1.Duration{Duration: renewBefore}
		}
	}

	if mcp.HTTP.TLS.Subject != nil {
		certSpec.Subject = &certv1.X509Subject{
			Organizations:       mcp.HTTP.TLS.Subject.Organizations,
			Countries:           mcp.HTTP.TLS.Subject.Countries,
			OrganizationalUnits: mcp.HTTP.TLS.Subject.OrganizationalUnits,
			Localities:          mcp.HTTP.TLS.Subject.Localities,
			Provinces:           mcp.HTTP.TLS.Subject.Provinces,
		}
	}

	if len(mcp.HTTP.TLS.Usages) > 0 {
		certSpec.Usages = make([]certv1.KeyUsage, len(mcp.HTTP.TLS.Usages))
		for i, usage := range mcp.HTTP.TLS.Usages {
			certSpec.Usages[i] = certv1.KeyUsage(usage)
		}
	} else {
		certSpec.Usages = []certv1.KeyUsage{
			certv1.UsageDigitalSignature,
			certv1.UsageKeyEncipherment,
			certv1.UsageServerAuth,
		}
	}

	return &certv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-mcp-tls", name),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: certSpec,
	}
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
			Path:     "/mcp",
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
		path = "/mcp"
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
	if operatorVersion := os.Getenv(mcpOperatorVersionEnv); operatorVersion != "" {
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
	if mcpTLSEnabled(spec) {
		return mcpHTTPPortTLSDefault
	}
	return mcpHTTPPortDefault
}

func mcpHTTPHost(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.HTTP != nil && spec.HTTP.Host != "" {
		return spec.HTTP.Host
	}
	return "0.0.0.0"
}

func mcpAllowedOrigins(spec *neo4jv1alpha1.MCPServerSpec) string {
	if spec != nil && spec.HTTP != nil {
		return spec.HTTP.AllowedOrigins
	}
	return ""
}

func mcpTLSEnabled(spec *neo4jv1alpha1.MCPServerSpec) bool {
	if spec == nil || mcpTransport(spec) != "http" || spec.HTTP == nil || spec.HTTP.TLS == nil {
		return false
	}
	mode := spec.HTTP.TLS.Mode
	if mode == "" {
		mode = "disabled"
	}
	return mode != "disabled"
}

func mcpTLSSecretName(spec *neo4jv1alpha1.MCPServerSpec, name string) string {
	if spec == nil || spec.HTTP == nil || spec.HTTP.TLS == nil {
		return ""
	}
	if spec.HTTP.TLS.SecretName != "" {
		return spec.HTTP.TLS.SecretName
	}
	return fmt.Sprintf("%s-mcp-tls-secret", name)
}

func mcpTLSVolume(spec *neo4jv1alpha1.MCPServerSpec, name string) ([]corev1.Volume, []corev1.VolumeMount) {
	if !mcpTLSEnabled(spec) {
		return nil, nil
	}

	secretName := mcpTLSSecretName(spec, name)
	if secretName == "" {
		return nil, nil
	}

	volume := corev1.Volume{
		Name: mcpTLSVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      mcpTLSVolumeName,
		MountPath: mcpTLSMountPath,
		ReadOnly:  true,
	}

	return []corev1.Volume{volume}, []corev1.VolumeMount{volumeMount}
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

func buildMCPEnv(spec *neo4jv1alpha1.MCPServerSpec, neo4jURI string, secretName, usernameKey, passwordKey string) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "NEO4J_URI", Value: neo4jURI},
		{Name: "NEO4J_READ_ONLY", Value: strconv.FormatBool(spec.ReadOnly)},
		{Name: "NEO4J_TELEMETRY", Value: strconv.FormatBool(spec.Telemetry)},
	}

	if spec.Database != "" {
		env = append(env, corev1.EnvVar{Name: "NEO4J_DATABASE", Value: spec.Database})
	}
	if spec.SchemaSampleSize != nil {
		env = append(env, corev1.EnvVar{Name: "NEO4J_SCHEMA_SAMPLE_SIZE", Value: strconv.Itoa(int(*spec.SchemaSampleSize))})
	}
	if spec.LogLevel != "" {
		env = append(env, corev1.EnvVar{Name: "NEO4J_LOG_LEVEL", Value: spec.LogLevel})
	}
	if spec.LogFormat != "" {
		env = append(env, corev1.EnvVar{Name: "NEO4J_LOG_FORMAT", Value: spec.LogFormat})
	}

	switch mcpTransport(spec) {
	case "http":
		env = append(env,
			corev1.EnvVar{Name: "NEO4J_MCP_TRANSPORT", Value: "http"},
			corev1.EnvVar{Name: "NEO4J_MCP_HTTP_HOST", Value: mcpHTTPHost(spec)},
			corev1.EnvVar{Name: "NEO4J_MCP_HTTP_PORT", Value: strconv.Itoa(int(mcpHTTPPort(spec)))},
		)
		if allowedOrigins := mcpAllowedOrigins(spec); allowedOrigins != "" {
			env = append(env, corev1.EnvVar{Name: "NEO4J_MCP_HTTP_ALLOWED_ORIGINS", Value: allowedOrigins})
		}
		if mcpTLSEnabled(spec) {
			env = append(env,
				corev1.EnvVar{Name: "NEO4J_MCP_HTTP_TLS_ENABLED", Value: "true"},
				corev1.EnvVar{Name: "NEO4J_MCP_HTTP_TLS_CERT_FILE", Value: mcpTLSCertFile},
				corev1.EnvVar{Name: "NEO4J_MCP_HTTP_TLS_KEY_FILE", Value: mcpTLSKeyFile},
			)
		}
	case "stdio":
		env = append(env,
			corev1.EnvVar{Name: "NEO4J_MCP_TRANSPORT", Value: "stdio"},
			corev1.EnvVar{
				Name: "NEO4J_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  usernameKey,
					},
				},
			},
			corev1.EnvVar{
				Name: "NEO4J_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  passwordKey,
					},
				},
			},
		)
	}

	env = append(env, filterMCPEnv(spec.Env)...)
	return env
}

func filterMCPEnv(env []corev1.EnvVar) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}

	reserved := map[string]struct{}{
		"NEO4J_URI":                      {},
		"NEO4J_USERNAME":                 {},
		"NEO4J_PASSWORD":                 {},
		"NEO4J_DATABASE":                 {},
		"NEO4J_READ_ONLY":                {},
		"NEO4J_TELEMETRY":                {},
		"NEO4J_LOG_LEVEL":                {},
		"NEO4J_LOG_FORMAT":               {},
		"NEO4J_SCHEMA_SAMPLE_SIZE":       {},
		"NEO4J_MCP_TRANSPORT":            {},
		"NEO4J_MCP_HTTP_HOST":            {},
		"NEO4J_MCP_HTTP_PORT":            {},
		"NEO4J_MCP_HTTP_ALLOWED_ORIGINS": {},
		"NEO4J_MCP_HTTP_TLS_ENABLED":     {},
		"NEO4J_MCP_HTTP_TLS_CERT_FILE":   {},
		"NEO4J_MCP_HTTP_TLS_KEY_FILE":    {},
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
