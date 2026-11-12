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

package studio

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

func studioContainerResources(s *neo4jv1.Neo4jStudio) corev1.ResourceRequirements {
	return Deployment(s, nil).Spec.Template.Spec.Containers[0].Resources
}

func TestDeploymentAppliesResources(t *testing.T) {
	s := &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			AssetStore: neo4jv1.StudioAssetStore{URI: "neo4j://store:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a"}}},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
			},
		},
	}
	r := studioContainerResources(s)
	// Explicit values win; the unset request/limit halves fall back to the operator defaults.
	if got := r.Requests.Cpu().String(); got != "300m" {
		t.Errorf("cpu request = %q, want 300m", got)
	}
	if got := r.Limits.Memory().String(); got != "2Gi" {
		t.Errorf("memory limit = %q, want 2Gi", got)
	}
	if r.Limits.Cpu().IsZero() || r.Requests.Memory().IsZero() {
		t.Errorf("unset halves must default, got %#v", r)
	}
}

// Omitting spec.resources must still yield a bounded container so a Studio pod can never run unbounded
// and starve its node (NEO-014).
func TestDeploymentDefaultsResourcesWhenOmitted(t *testing.T) {
	s := &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			AssetStore: neo4jv1.StudioAssetStore{URI: "neo4j://store:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a"}}},
		},
	}
	r := studioContainerResources(s)
	if r.Limits.Cpu().IsZero() || r.Limits.Memory().IsZero() || r.Requests.Cpu().IsZero() || r.Requests.Memory().IsZero() {
		t.Fatalf("omitted spec.resources must default cpu+memory requests and limits, got %#v", r)
	}
}

func plainEnv(env []corev1.EnvVar) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		if e.ValueFrom == nil {
			m[e.Name] = e.Value
		}
	}
	return m
}

func secretEnvRef(env []corev1.EnvVar, name string) *corev1.SecretKeySelector {
	for _, e := range env {
		if e.Name == name && e.ValueFrom != nil {
			return e.ValueFrom.SecretKeyRef
		}
	}
	return nil
}

func TestEnvVarsBuildsNESConfig(t *testing.T) {
	bloomOff := false
	basicOn := true
	s := &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			Version:    "2025.01",
			License:    &neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "lic"}},
			AssetStore: neo4jv1.StudioAssetStore{URI: "neo4j://store:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "store-auth"}}},
			Tools:      &neo4jv1.StudioToolsSpec{Bloom: &bloomOff},
			Deployments: []neo4jv1.StudioDeployment{
				{Name: "Prod Graph", Mode: neo4jv1.StudioModeReverseProxy},
				{Name: "Lab", Mode: neo4jv1.StudioModeDirect,
					Auth: &neo4jv1.StudioDeploymentAuth{Basic: &basicOn},
					RoleMapping: []neo4jv1.StudioRoleMapping{{
						Role:          neo4jv1.StudioRoleAdmin,
						DatabaseRoles: []string{"admin"},
					}}},
			},
			ConfigFromSecrets: []neo4jv1.StudioSecretConfig{{
				Name:      "NES_generativeAi_chatCompletionProvider_default_upstream_apiKey",
				SecretRef: neo4jv1.StudioSecretKeyRef{Name: "genai", Key: "apiKey"},
			}},
			Config: map[string]string{"NES_mapTiles_enabled": "false"},
		},
	}
	resolved := []ResolvedDeployment{
		{Name: "Prod Graph", URI: "http://g.ns.svc:7474", Mode: string(neo4jv1.StudioModeReverseProxy)},
		{Name: "Lab", URI: "http://lab:7474", PublicURI: "bolt://lab:7687", Mode: string(neo4jv1.StudioModeDirect)},
	}

	env := EnvVars(s, resolved)
	got := plainEnv(env)

	want := map[string]string{
		"NES_server_port":                 "8080",
		"NES_assetStore_default_uri":      "neo4j://store:7687",
		"NES_assetStore_default_database": "assets",
		"NES_license_path":                "/licenses/nes.license",
		"NES_neo4jDeployments_d0_name":    "Prod Graph",
		"NES_neo4jDeployments_d0_uri":     "http://g.ns.svc:7474",
		"NES_neo4jDeployments_d1_name":    "Lab",
		"NES_neo4jDeployments_d1_uri":     "http://lab:7474",
		// Direct (d1) must enable directClientQuery and set the Bolt publicUri; ReverseProxy (d0) must not.
		"NES_neo4jDeployments_d1_directClientQuery_enabled":    "true",
		"NES_neo4jDeployments_d1_directClientQuery_publicUri":  "bolt://lab:7687",
		"NES_neo4jDeployments_d1_authentication_basic_enabled": "true",
		"NES_neo4jDeployments_d1_authorization_roleMapping":    `[{"role":"studioAdmin","members":[{"kind":"databaseRole","name":"admin"}]}]`,
		"NES_tools_bloom_enabled":                              "false",
		"NES_mapTiles_enabled":                                 "false",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env %s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["NES_neo4jDeployments_d0_directClientQuery_enabled"]; ok {
		t.Errorf("ReverseProxy deployment must not enable directClientQuery")
	}

	// Asset-store credentials and secret-passthrough must be secretKeyRefs, never plaintext.
	u := secretEnvRef(env, "NES_assetStore_default_authentication_basic_username")
	p := secretEnvRef(env, "NES_assetStore_default_authentication_basic_password")
	if u == nil || u.Name != "store-auth" || u.Key != "username" {
		t.Errorf("username secret ref = %+v", u)
	}
	if p == nil || p.Name != "store-auth" || p.Key != "password" {
		t.Errorf("password secret ref = %+v", p)
	}
	k := secretEnvRef(env, "NES_generativeAi_chatCompletionProvider_default_upstream_apiKey")
	if k == nil || k.Name != "genai" || k.Key != "apiKey" {
		t.Errorf("genai apiKey secret ref = %+v", k)
	}
}

func TestImageRefDefaultsVersion(t *testing.T) {
	// Version omitted → operator's pinned Studio tag (BDR-015 usability revision).
	s := &neo4jv1.Neo4jStudio{ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"}}
	if got := ImageRef(s); got != DefaultImageRepository+":"+DefaultVersion {
		t.Fatalf("ImageRef default = %q, want %q", got, DefaultImageRepository+":"+DefaultVersion)
	}
	// Explicit version still wins.
	s.Spec.Version = "2025.09"
	if got := ImageRef(s); got != "neo4j/enterprise-studio:2025.09" {
		t.Fatalf("ImageRef explicit = %q", got)
	}
}

func TestTLSAndSecurityOverride(t *testing.T) {
	uid := int64(1000)
	s := &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			Version:    "2025.01",
			AssetStore:   neo4jv1.StudioAssetStore{URI: "neo4j://store:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a"}}},
			Connectivity: &neo4jv1.StudioConnectivitySpec{TLS: &neo4jv1.StudioServerTLS{Enabled: true, SecretRef: neo4jv1.SecretRef{Name: "studio-tls"}}},
			Security:   &neo4jv1.StudioSecuritySpec{ContainerSecurityContext: &corev1.SecurityContext{RunAsUser: &uid}},
		},
	}
	dep := Deployment(s, nil)
	got := plainEnv(EnvVars(s, nil))
	if got["NES_server_https_enabled"] != "true" || got["NES_server_https_certificates_privateKey"] != "tls.key" {
		t.Errorf("TLS env not set: %#v", got)
	}
	// The override UID must win, but privilege escalation must stay disabled.
	c := dep.Spec.Template.Spec.Containers[0]
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 1000 {
		t.Errorf("override runAsUser not applied: %#v", c.SecurityContext.RunAsUser)
	}
	if c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Errorf("allowPrivilegeEscalation must stay false")
	}
	// HTTPS readiness probe scheme follows TLS.
	if c.ReadinessProbe.HTTPGet.Scheme != corev1.URISchemeHTTPS {
		t.Errorf("probe scheme = %q, want HTTPS", c.ReadinessProbe.HTTPGet.Scheme)
	}
}

func TestDeploymentIsHardenedAndStateless(t *testing.T) {
	s := &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			Version:    "2025.01",
			AssetStore: neo4jv1.StudioAssetStore{URI: "neo4j://store:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "store-auth"}}},
		},
	}
	dep := Deployment(s, []ResolvedDeployment{{Name: "g", URI: "http://g.ns.svc:7474", Mode: string(neo4jv1.StudioModeReverseProxy)}})

	pod := dep.Spec.Template.Spec
	// Service-link env injection must be off, or a Service named e.g. nes-storage injects
	// NES_STORAGE_* vars that collide with Studio's NES_* config prefix and break config load.
	if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
		t.Fatalf("enableServiceLinks must be false, got %#v", pod.EnableServiceLinks)
	}
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatalf("pod must set runAsNonRoot for restricted PodSecurity, got %#v", pod.SecurityContext)
	}
	// The image defaults to root; a non-root runAsUser is required or the kubelet refuses it.
	if pod.SecurityContext.RunAsUser == nil || *pod.SecurityContext.RunAsUser == 0 {
		t.Fatalf("pod must pin a non-root runAsUser, got %#v", pod.SecurityContext.RunAsUser)
	}
	c := pod.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.Capabilities == nil || len(c.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatalf("container must drop capabilities, got %#v", c.SecurityContext)
	}
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser == 0 {
		t.Fatalf("container must pin a non-root runAsUser, got %#v", c.SecurityContext.RunAsUser)
	}
	// Stateless: no PVCs, no config volume — only the (absent here) optional license volume.
	if len(pod.Volumes) != 0 {
		t.Errorf("expected no volumes without a license, got %#v", pod.Volumes)
	}
	if c.Image != "neo4j/enterprise-studio:2025.01" {
		t.Errorf("image = %q", c.Image)
	}
}
