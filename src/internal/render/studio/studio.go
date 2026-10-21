/*
Copyright Neo4j.

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

// Package studio renders the Kubernetes objects for an Enterprise Studio deployment (ADR-018):
// a stateless Deployment plus its Service, ServiceAccount, ConfigMap and optional Ingress. Pure
// builders — no client, no I/O. The caller (domain/studio) resolves deployment URIs and applies.
package studio

import (
	"encoding/json"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	// DefaultImageRepository is the published Enterprise Studio image, used when spec.image.repository
	// is unset (hub.docker.com/r/neo4j/enterprise-studio).
	DefaultImageRepository = "neo4j/enterprise-studio"
	// DefaultPort is NES_server_port's default (container port 8080).
	DefaultPort = int32(8080)

	containerName = "studio"
	// AppName is the app.kubernetes.io/name for Studio objects (distinct from the Neo4j workload).
	AppName        = "neo4j-studio"
	componentValue = "studio"

	licenseMountPath = "/licenses"
	licenseVolume    = "license"
	// licenseFileName is the key the operator expects in the license Secret, mounted at
	// licenseMountPath/licenseFileName and pointed at by NES_license_path (matches the NES docs).
	licenseFileName = "nes.license"

	tlsVolume    = "tls"
	tlsMountPath = "/certificates/https"
	// A kubernetes.io/tls Secret stores the cert/key under these fixed keys.
	tlsCertFile = "tls.crt"
	tlsKeyFile  = "tls.key"
)

// ResolvedDeployment is one served deployment with its resolved URIs (domain/studio fills these from
// an in-cluster neo4jRef or the external uri verbatim). URI is the server-reachable endpoint NES uses;
// PublicURI is the browser-reachable Bolt endpoint for Direct mode (directClientQuery), empty otherwise.
type ResolvedDeployment struct {
	Name      string
	URI       string
	PublicURI string
	Mode      string
}

// Name returns the shared name for all Studio child objects (ADR-005 — deterministic).
func Name(s *neo4jv1.Neo4jStudio) string { return s.Name }

// Labels returns the common labels on every Studio object.
func Labels(s *neo4jv1.Neo4jStudio) map[string]string {
	return map[string]string{
		render.LabelName:      AppName,
		render.LabelInstance:  s.Name,
		render.LabelManagedBy: render.ManagedByValue,
		render.LabelComponent: componentValue,
	}
}

// SelectorLabels is the minimal, stable set for the Deployment selector and Service.
func SelectorLabels(s *neo4jv1.Neo4jStudio) map[string]string {
	return map[string]string{
		render.LabelName:     AppName,
		render.LabelInstance: s.Name,
	}
}

// ImageRef returns the effective Studio image reference (digest pin wins over version tag).
func ImageRef(s *neo4jv1.Neo4jStudio) string {
	repo := DefaultImageRepository
	if s.Spec.Image != nil && s.Spec.Image.Repository != "" {
		repo = s.Spec.Image.Repository
	}
	if s.Spec.Image != nil && s.Spec.Image.Digest != "" {
		return repo + "@" + s.Spec.Image.Digest
	}
	return repo + ":" + s.Spec.Version
}

func pullPolicy(s *neo4jv1.Neo4jStudio) corev1.PullPolicy {
	if s.Spec.Image != nil && s.Spec.Image.PullPolicy != "" {
		return s.Spec.Image.PullPolicy
	}
	return corev1.PullIfNotPresent
}

// Port returns the Studio container/Service port (NES_server_port, default 8080).
func Port(s *neo4jv1.Neo4jStudio) int32 {
	if v, ok := s.Spec.Config["NES_server_port"]; ok {
		if p := parsePort(v); p > 0 {
			return p
		}
	}
	return DefaultPort
}

func replicas(s *neo4jv1.Neo4jStudio) int32 {
	if s.Spec.Replicas != nil && *s.Spec.Replicas > 0 {
		return *s.Spec.Replicas
	}
	return 1
}

// Deployment builds the stateless Studio Deployment (no PVC — ADR-018). All configuration is driven
// through NES_* environment variables, which override the image's bundled config.yaml — the operator
// does not mount a config file (the recommended approach for simple deployments per the NES docs).
func Deployment(s *neo4jv1.Neo4jStudio, resolved []ResolvedDeployment) *appsv1.Deployment {
	reps := replicas(s)
	port := Port(s)

	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount
	// Optional license as a mounted file; Studio starts without it (BDR-015 §3).
	if s.Spec.License != nil && s.Spec.License.SecretRef.Name != "" {
		volumes = append(volumes, corev1.Volume{
			Name:         licenseVolume,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: s.Spec.License.SecretRef.Name}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: licenseVolume, MountPath: licenseMountPath, ReadOnly: true})
	}
	// Optional HTTPS: mount the kubernetes.io/tls Secret so NES can read the cert/key files.
	if tlsEnabled(s) {
		volumes = append(volumes, corev1.Volume{
			Name:         tlsVolume,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: s.Spec.Server.TLS.SecretRef.Name}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: tlsVolume, MountPath: tlsMountPath, ReadOnly: true})
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: Name(s), Namespace: s.Namespace, Labels: Labels(s)},
		Spec: appsv1.DeploymentSpec{
			Replicas: &reps,
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(s)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: Labels(s)},
				Spec: corev1.PodSpec{
					ServiceAccountName: Name(s),
					ImagePullSecrets:   pullSecrets(s),
					Volumes:            volumes,
					// Studio reads EVERY NES_* env var as configuration. Kubernetes' per-Service
					// discovery vars (e.g. a Service named "nes-storage" injects NES_STORAGE_SERVICE_HOST)
					// collide with that prefix and make NES fail config load ("invalid keys: STORAGE").
					// Studio reaches dependencies via DNS/config URIs, not these vars, so disable them.
					EnableServiceLinks: ptrBool(false),
					SecurityContext:    podSecurityContext(s),
					Containers: []corev1.Container{{
						Name:            containerName,
						Image:           ImageRef(s),
						ImagePullPolicy: pullPolicy(s),
						Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: port}},
						Env:             EnvVars(s, resolved),
						VolumeMounts:    mounts,
						ReadinessProbe:  probe(s, port),
						SecurityContext: containerSecurityContext(s),
					}},
				},
			},
		},
	}
}

// deploymentID returns the alphanumeric NES deployment id for the i-th served deployment. NES requires
// ids to be letters/digits only; the human display name rides in NES_neo4jDeployments_<id>_name, so an
// index-based id (d0, d1, …) is safe, unique and deterministic. ponytail: swap for a sanitized name if
// stable ids across reorders ever matter.
func deploymentID(i int) string { return "d" + strconv.Itoa(i) }

// EnvVars assembles the full NES_* configuration for the container: server port + TLS, asset store
// (+ secret credentials), each served deployment (uri/publicUri, auth toggles, roleMapping), tools,
// the optional license path, and both the plaintext (spec.config) and secret (spec.configFromSecrets)
// passthrough. Deterministically ordered so re-renders don't churn the Deployment.
func EnvVars(s *neo4jv1.Neo4jStudio, resolved []ResolvedDeployment) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "NES_server_port", Value: itoa(Port(s))},
		{Name: "NES_assetStore_default_uri", Value: s.Spec.AssetStore.URI},
		{Name: "NES_assetStore_default_database", Value: s.Spec.AssetStore.Database},
	}
	if sr := s.Spec.AssetStore.Auth.SecretRef.Name; sr != "" {
		env = append(env,
			secretEnv("NES_assetStore_default_authentication_basic_username", sr, "username"),
			secretEnv("NES_assetStore_default_authentication_basic_password", sr, "password"),
		)
	}
	if s.Spec.License != nil && s.Spec.License.SecretRef.Name != "" {
		env = append(env, corev1.EnvVar{Name: "NES_license_path", Value: licenseMountPath + "/" + licenseFileName})
	}
	if tlsEnabled(s) {
		env = append(env,
			corev1.EnvVar{Name: "NES_server_https_enabled", Value: "true"},
			corev1.EnvVar{Name: "NES_server_https_certificates_baseDirectory", Value: tlsMountPath},
			corev1.EnvVar{Name: "NES_server_https_certificates_privateKey", Value: tlsKeyFile},
			corev1.EnvVar{Name: "NES_server_https_certificates_publicCertificate", Value: tlsCertFile},
		)
	}
	env = append(env, toolsEnv(s)...)

	for i, d := range resolved {
		id := deploymentID(i)
		env = append(env,
			corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_name", Value: d.Name},
			corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_uri", Value: d.URI},
		)
		// Direct mode = the browser queries the DBMS directly over Bolt. Enable directClientQuery and
		// set publicUri (the browser-reachable Bolt endpoint) — NES requires publicUri to be set when
		// directClientQuery is enabled. ReverseProxy leaves both at their defaults (proxied over HTTP).
		if d.Mode == string(neo4jv1.StudioModeDirect) {
			env = append(env,
				corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_directClientQuery_enabled", Value: "true"},
				corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_directClientQuery_publicUri", Value: d.PublicURI},
			)
		}
		// resolved[i] is 1:1 with spec.Deployments[i]; read the non-resolved fields from the spec.
		if i >= len(s.Spec.Deployments) {
			continue
		}
		spec := s.Spec.Deployments[i]
		if spec.Auth != nil {
			if spec.Auth.Basic != nil {
				env = append(env, corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_authentication_basic_enabled", Value: boolStr(*spec.Auth.Basic)})
			}
			if spec.Auth.OIDC != nil {
				env = append(env, corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_authentication_oidc_enabled", Value: boolStr(*spec.Auth.OIDC)})
			}
		}
		if len(spec.RoleMapping) > 0 {
			if js, err := json.Marshal(spec.RoleMapping); err == nil {
				env = append(env, corev1.EnvVar{Name: "NES_neo4jDeployments_" + id + "_authorization_roleMapping", Value: string(js)})
			}
		}
	}

	// Secret-valued passthrough: each becomes a secretKeyRef env (ordered, deterministic).
	for _, sc := range s.Spec.ConfigFromSecrets {
		env = append(env, secretEnv(sc.Name, sc.SecretRef.Name, sc.SecretRef.Key))
	}
	// Plaintext passthrough of any non-secret NES_* keys, except the port which is modelled above.
	keys := make([]string, 0, len(s.Spec.Config))
	for k := range s.Spec.Config {
		if k != "NES_server_port" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, corev1.EnvVar{Name: k, Value: s.Spec.Config[k]})
	}
	return env
}

// toolsEnv emits NES_tools_*_enabled only for tools the user explicitly toggled.
func toolsEnv(s *neo4jv1.Neo4jStudio) []corev1.EnvVar {
	t := s.Spec.Tools
	if t == nil {
		return nil
	}
	var env []corev1.EnvVar
	if t.Dashboards != nil {
		env = append(env, corev1.EnvVar{Name: "NES_tools_dashboards_enabled", Value: boolStr(*t.Dashboards)})
	}
	if t.Bloom != nil {
		env = append(env, corev1.EnvVar{Name: "NES_tools_bloom_enabled", Value: boolStr(*t.Bloom)})
	}
	if t.Query != nil {
		env = append(env, corev1.EnvVar{Name: "NES_tools_query_enabled", Value: boolStr(*t.Query)})
	}
	return env
}

func tlsEnabled(s *neo4jv1.Neo4jStudio) bool {
	return s.Spec.Server != nil && s.Spec.Server.TLS != nil && s.Spec.Server.TLS.Enabled && s.Spec.Server.TLS.SecretRef.Name != ""
}

func boolStr(b bool) string { return strconv.FormatBool(b) }

// Service fronts the Studio port. When any served deployment is ReverseProxy, the Service is made
// sticky (sessionAffinity: ClientIP) — the Studio↔cluster HTTP-session prerequisite (ADR-018).
func Service(s *neo4jv1.Neo4jStudio, resolved []ResolvedDeployment) *corev1.Service {
	port := Port(s)
	svcType := corev1.ServiceTypeClusterIP
	var annotations map[string]string
	if s.Spec.Connectivity != nil && s.Spec.Connectivity.Service != nil {
		if t := s.Spec.Connectivity.Service.Type; t != "" {
			svcType = corev1.ServiceType(t)
		}
		annotations = s.Spec.Connectivity.Service.Annotations
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: Name(s), Namespace: s.Namespace, Labels: Labels(s), Annotations: annotations},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: SelectorLabels(s),
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       port,
				TargetPort: intstr.FromInt32(port),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	if NeedsSessionAffinity(resolved) {
		svc.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	}
	return svc
}

// NeedsSessionAffinity reports whether any served deployment uses ReverseProxy (ADR-018).
func NeedsSessionAffinity(resolved []ResolvedDeployment) bool {
	for _, d := range resolved {
		if d.Mode == string(neo4jv1.StudioModeReverseProxy) {
			return true
		}
	}
	return false
}

// ServiceAccount is the Studio pod identity (no special RBAC — Studio talks Bolt/HTTP, not the API).
func ServiceAccount(s *neo4jv1.Neo4jStudio) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: Name(s), Namespace: s.Namespace, Labels: Labels(s)},
	}
}

// Ingress builds the optional Studio Ingress, or nil when none is configured (ADR-018). When any
// served deployment is ReverseProxy, a sticky-session annotation is added (prerequisite).
func Ingress(s *neo4jv1.Neo4jStudio, resolved []ResolvedDeployment) *networkingv1.Ingress {
	if s.Spec.Connectivity == nil || s.Spec.Connectivity.Ingress == nil || len(s.Spec.Connectivity.Ingress.Rules) == 0 {
		return nil
	}
	in := s.Spec.Connectivity.Ingress
	port := Port(s)
	pathType := networkingv1.PathTypePrefix
	var rules []networkingv1.IngressRule
	for _, r := range in.Rules {
		rules = append(rules, networkingv1.IngressRule{
			Host: r.Host,
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{{
					Path:     "/",
					PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: Name(s),
						Port: networkingv1.ServiceBackendPort{Number: port},
					}},
				}},
			}},
		})
	}
	annotations := map[string]string{}
	for k, v := range in.Annotations {
		annotations[k] = v
	}
	if NeedsSessionAffinity(resolved) {
		// nginx sticky cookie — the ReverseProxy HTTP-session prerequisite at the Ingress (ADR-018).
		annotations["nginx.ingress.kubernetes.io/affinity"] = "cookie"
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: Name(s), Namespace: s.Namespace, Labels: Labels(s), Annotations: annotations},
		Spec:       networkingv1.IngressSpec{Rules: rules},
	}
	if in.ClassName != "" {
		ing.Spec.IngressClassName = &in.ClassName
	}
	for _, t := range in.TLS {
		ing.Spec.TLS = append(ing.Spec.TLS, networkingv1.IngressTLS{Hosts: t.Hosts, SecretName: t.SecretName})
	}
	return ing
}

// studioUID/studioGID: the neo4j/enterprise-studio image ships no USER and defaults to root, but its
// entrypoint only writes to /app when LICENSE_KEY is set (the operator mounts the license as a file
// instead) and /app + ./nes are world-readable/executable, so it runs fine as an arbitrary non-root
// user — verified by running the published image as this UID. We pin the operand's 7474 for house
// consistency; a specific runAsUser is required for restricted PodSecurity, which refuses to start a
// root image. ponytail: expose spec.security overrides if a cluster ever needs a different UID.
const (
	studioUID = int64(7474)
	studioGID = int64(7474)
)

// podSecurityContext and containerSecurityContext harden the Studio pod so it passes restricted Pod
// Security / policy admission (the same gate the operand and backup Job clear) and runs as non-root.
// spec.security fields, when set, are merged over these defaults (privileged/privilege-escalation stay
// disabled regardless) — mirroring the Neo4j workload's override behaviour.
func podSecurityContext(s *neo4jv1.Neo4jStudio) *corev1.PodSecurityContext {
	runAsNonRoot := true
	uid, gid := studioUID, studioGID
	out := &corev1.PodSecurityContext{
		RunAsNonRoot:   &runAsNonRoot,
		RunAsUser:      &uid,
		RunAsGroup:     &gid,
		FSGroup:        &gid,
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if s.Spec.Security == nil || s.Spec.Security.PodSecurityContext == nil {
		return out
	}
	u := s.Spec.Security.PodSecurityContext
	if u.RunAsUser != nil {
		out.RunAsUser = u.RunAsUser
	}
	if u.RunAsGroup != nil {
		out.RunAsGroup = u.RunAsGroup
	}
	if u.FSGroup != nil {
		out.FSGroup = u.FSGroup
	}
	if u.RunAsNonRoot != nil {
		out.RunAsNonRoot = u.RunAsNonRoot
	}
	if u.FSGroupChangePolicy != nil {
		out.FSGroupChangePolicy = u.FSGroupChangePolicy
	}
	if u.SeccompProfile != nil {
		out.SeccompProfile = u.SeccompProfile.DeepCopy()
	}
	if u.SupplementalGroups != nil {
		out.SupplementalGroups = u.SupplementalGroups
	}
	return out
}

func containerSecurityContext(s *neo4jv1.Neo4jStudio) *corev1.SecurityContext {
	runAsNonRoot := true
	allowPrivEsc := false
	privileged := false
	uid, gid := studioUID, studioGID
	out := &corev1.SecurityContext{
		RunAsNonRoot:             &runAsNonRoot,
		RunAsUser:                &uid,
		RunAsGroup:               &gid,
		Privileged:               &privileged,
		AllowPrivilegeEscalation: &allowPrivEsc,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if s.Spec.Security == nil || s.Spec.Security.ContainerSecurityContext == nil {
		return out
	}
	u := s.Spec.Security.ContainerSecurityContext
	if u.RunAsUser != nil {
		out.RunAsUser = u.RunAsUser
	}
	if u.RunAsGroup != nil {
		out.RunAsGroup = u.RunAsGroup
	}
	if u.RunAsNonRoot != nil {
		out.RunAsNonRoot = u.RunAsNonRoot
	}
	if u.ReadOnlyRootFilesystem != nil {
		out.ReadOnlyRootFilesystem = u.ReadOnlyRootFilesystem
	}
	if u.Capabilities != nil {
		out.Capabilities = u.Capabilities.DeepCopy()
	}
	if u.SeccompProfile != nil {
		out.SeccompProfile = u.SeccompProfile.DeepCopy()
	}
	// privileged and allowPrivilegeEscalation stay false regardless of the override.
	return out
}

func pullSecrets(s *neo4jv1.Neo4jStudio) []corev1.LocalObjectReference {
	if s.Spec.Image == nil || len(s.Spec.Image.PullSecrets) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, 0, len(s.Spec.Image.PullSecrets))
	for _, n := range s.Spec.Image.PullSecrets {
		out = append(out, corev1.LocalObjectReference{Name: n})
	}
	return out
}

func secretEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secret},
			Key:                  key,
		},
	}}
}

func ptrBool(b bool) *bool { return &b }

func itoa(p int32) string { return strconv.Itoa(int(p)) }

func parsePort(v string) int32 {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return int32(n)
}

func probe(s *neo4jv1.Neo4jStudio, port int32) *corev1.Probe {
	scheme := corev1.URISchemeHTTP
	if tlsEnabled(s) {
		scheme = corev1.URISchemeHTTPS
	}
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
			Path:   "/",
			Port:   intstr.FromInt32(port),
			Scheme: scheme,
		}},
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
	}
}
