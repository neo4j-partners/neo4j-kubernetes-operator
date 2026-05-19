/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package resources_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// pluginWithSource returns a minimal Neo4jPlugin spec'd for
// VerifiedDownload, with optional authSecret. Centralised so each
// test case stays focused on the one shape it's asserting.
func pluginWithSource(name, url, checksum, authSecret string) *neo4jv1beta1.Neo4jPlugin {
	return &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			Name:        name,
			Version:     "1.0.0",
			InstallMode: "VerifiedDownload",
			Source: &neo4jv1beta1.PluginSource{
				Type:       "url",
				URL:        url,
				Checksum:   checksum,
				AuthSecret: authSecret,
			},
		},
	}
}

// TestBuildPluginVerifiedDownloadInitContainer_DefaultImage locks in
// the baked-in default when the caller passes "" — air-gapped clusters
// override via the helm value but everyone else gets curlimages/curl.
func TestBuildPluginVerifiedDownloadInitContainer_DefaultImage(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://example.com/gds.jar", validSHA256, "")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", nil)

	assert.Equal(t, resources.PluginInitContainerName("gds"), c.Name)
	assert.Equal(t, resources.DefaultPluginInitContainerImage, c.Image,
		"empty image override must fall back to baked-in default")
}

// TestBuildPluginVerifiedDownloadInitContainer_ImageOverride confirms
// the chart override flows through. Without this the air-gap escape
// hatch from the helm value silently fails.
func TestBuildPluginVerifiedDownloadInitContainer_ImageOverride(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://example.com/gds.jar", validSHA256, "")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "internal.mirror/curl:8.5.0", nil)

	assert.Equal(t, "internal.mirror/curl:8.5.0", c.Image)
}

// TestBuildPluginVerifiedDownloadInitContainer_EnvVarsCarryCheckpoint
// verifies the four env vars the init script reads are present with
// the expected values. The script depends on this contract — break it
// and the script either no-ops or downloads the wrong thing.
func TestBuildPluginVerifiedDownloadInitContainer_EnvVarsCarryCheckpoint(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://example.com/gds.jar", validSHA256, "")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", nil)

	envMap := map[string]string{}
	for _, e := range c.Env {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "gds", envMap["PLUGIN_NAME"])
	assert.Equal(t, "https://example.com/gds.jar", envMap["PLUGIN_URL"])
	assert.Equal(t, validSHA256, envMap["PLUGIN_CHECKSUM"])
	assert.Equal(t, "/plugins/gds.jar", envMap["PLUGIN_TARGET"])
}

// TestBuildPluginVerifiedDownloadInitContainer_NoAuthNoCA asserts the
// minimal-volume case: no authSecret, no trustedCASecrets. Only the
// shared /plugins emptyDir mount should appear. Anything more is
// scope creep that grows the pod spec for no reason.
func TestBuildPluginVerifiedDownloadInitContainer_NoAuthNoCA(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://example.com/gds.jar", validSHA256, "")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", nil)

	require.Len(t, c.VolumeMounts, 1, "no auth + no CA must produce only the /plugins mount")
	assert.Equal(t, "/plugins", c.VolumeMounts[0].MountPath)
	// Script must not reference the auth/ca branches.
	assert.NotContains(t, c.Args[1], "AUTH_HEADER", "auth branch must be absent without authSecret")
	assert.NotContains(t, c.Args[1], "ca-bundle.crt", "CA branch must be absent without trustedCASecrets")
}

// TestBuildPluginVerifiedDownloadInitContainer_WithAuth verifies the
// authSecret volume mount + the script's auth-header branch land
// together. Half-wiring (mount without script, or script without
// mount) would silently fall back to anonymous downloads.
func TestBuildPluginVerifiedDownloadInitContainer_WithAuth(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://artifactory.example.com/gds.jar", validSHA256, "artifactory-creds")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", nil)

	var foundAuth bool
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/etc/plugin-auth" {
			foundAuth = true
		}
	}
	assert.True(t, foundAuth, "authSecret must produce an /etc/plugin-auth mount")
	script := c.Args[1]
	assert.Contains(t, script, "/etc/plugin-auth/header")
	assert.Contains(t, script, "/etc/plugin-auth/token")
	assert.Contains(t, script, "Bearer")
}

// TestBuildPluginVerifiedDownloadInitContainer_WithCA verifies that
// when trustedCASecrets are supplied, the script concatenates them
// and points curl at the bundle. This is the path that lets internal
// Artifactory behind a corporate CA work without manual init-image
// customisation.
func TestBuildPluginVerifiedDownloadInitContainer_WithCA(t *testing.T) {
	const validSHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	plugin := pluginWithSource("gds", "https://internal/gds.jar", validSHA256, "")
	caSecrets := []neo4jv1beta1.TrustedCASecret{
		{Name: "corp-root-ca"},
		{Name: "intermediate-ca", Key: "tls.crt"},
	}

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", caSecrets)

	var foundCA bool
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/etc/plugin-ca" {
			foundCA = true
		}
	}
	assert.True(t, foundCA, "trustedCASecrets must produce an /etc/plugin-ca mount")
	script := c.Args[1]
	assert.Contains(t, script, "/etc/plugin-ca/*.crt")
	assert.Contains(t, script, "--cacert")
	assert.Contains(t, script, "ca-bundle.crt")
}

// TestBuildPluginCAVolume_PerSecretKey asserts the projected volume
// honours per-Secret Key overrides (default "ca.crt"). cert-manager-
// issued Secrets use that exact key, so the default works; custom
// providers (Vault PKI, etc.) often emit tls.crt instead.
func TestBuildPluginCAVolume_PerSecretKey(t *testing.T) {
	caSecrets := []neo4jv1beta1.TrustedCASecret{
		{Name: "corp-root-ca"},                    // implicit ca.crt
		{Name: "intermediate-ca", Key: "tls.crt"}, // explicit override
	}
	vol := resources.BuildPluginCAVolume(caSecrets)

	require.NotNil(t, vol)
	require.NotNil(t, vol.Projected)
	require.Len(t, vol.Projected.Sources, 2)

	require.Len(t, vol.Projected.Sources[0].Secret.Items, 1)
	assert.Equal(t, "ca.crt", vol.Projected.Sources[0].Secret.Items[0].Key,
		"missing Key on TrustedCASecret must default to ca.crt")
	assert.Equal(t, "corp-root-ca.crt", vol.Projected.Sources[0].Secret.Items[0].Path)

	require.Len(t, vol.Projected.Sources[1].Secret.Items, 1)
	assert.Equal(t, "tls.crt", vol.Projected.Sources[1].Secret.Items[0].Key)
	assert.Equal(t, "intermediate-ca.crt", vol.Projected.Sources[1].Secret.Items[0].Path)
}

// TestPluginInitScript_ChecksumAlgoBranching verifies the script
// dispatches to sha256sum vs sha512sum based on the checksum prefix.
// Locking this in via test means a future refactor of the script
// can't silently fall back to sha256 for a sha512:-prefixed value
// (which would still pass the validator but mismatch every time).
func TestPluginInitScript_ChecksumAlgoBranching(t *testing.T) {
	plugin := pluginWithSource("gds", "https://x/y.jar",
		"sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e", "")

	c := resources.BuildPluginVerifiedDownloadInitContainer(plugin, "", nil)
	script := c.Args[1]

	// Script must reference both algos in the case-statement (sha256
	// for backwards compat with most published checksums, sha512 for
	// the user's choice). We don't test against a downloaded file —
	// that's a live integration test, not a unit test. Lock in the
	// case statement instead.
	assert.True(t,
		strings.Contains(script, "sha256:*)") && strings.Contains(script, "sha512:*)"),
		"script must dispatch on both sha256: and sha512: prefixes")
	assert.Contains(t, script, "sha256sum")
	assert.Contains(t, script, "sha512sum")
}
