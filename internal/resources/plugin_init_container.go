/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package resources

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// DefaultPluginInitContainerImage is the curl+sha256sum image baked
// into the resource builder when no override is provided. Helm exposes
// Values.pluginInitContainer.image which is forwarded to the operator
// via the PLUGIN_INIT_CONTAINER_IMAGE env var (downward to
// cmd/main.go); air-gapped clusters point it at a mirror.
//
// curlimages/curl ships busybox under the hood, which gives us
// sha256sum and sha512sum without a second binary. Pinned to a digest
// would be even better, but the chart override is the right escape
// hatch for users who want that level of pinning.
const DefaultPluginInitContainerImage = "curlimages/curl:8.5.0"

// PluginInitContainerName returns the deterministic name of the init
// container the operator injects for a VerifiedDownload plugin. The
// name is used both as the container's metadata.name AND as the value
// tracked in the StatefulSet's neo4j.com/plugin-init-containers
// annotation so the plugin controller can remove its own containers
// on uninstall without disturbing foreign init containers (e.g.
// truststore JKS builder injected by the cluster controller).
func PluginInitContainerName(pluginName string) string {
	// Container names must be a DNS-1123 label. The Neo4jPlugin's
	// spec.name is already validated to that subset, so no further
	// sanitisation is needed.
	return "plugin-init-" + pluginName
}

// BuildPluginVerifiedDownloadInitContainer returns the init container
// spec that downloads a single plugin's JAR, verifies its checksum
// against spec.source.checksum, and drops the file into the shared
// /plugins emptyDir before the Neo4j entrypoint runs.
//
//   - image     overrides DefaultPluginInitContainerImage when non-empty
//     (sourced from the operator's PLUGIN_INIT_CONTAINER_IMAGE env var).
//   - caSecrets is the cluster/standalone's TrustedCASecrets list. Each
//     Secret's CA bundle is mounted under /etc/plugin-ca and curl is
//     pointed at the resulting concatenated file. Empty list = system
//     CA only.
//
// The script behaviour:
//
//  1. Reads PLUGIN_URL, PLUGIN_CHECKSUM, PLUGIN_TARGET from env.
//  2. Optionally adds an Authorization header from /etc/plugin-auth/token
//     when source.authSecret is set (mounted by the caller).
//  3. curl --fail --location -o $PLUGIN_TARGET (with optional --cacert
//     when CA bundle present).
//  4. Computes sha256sum or sha512sum based on the checksum prefix.
//  5. Compares against the expected value. Mismatch -> exit non-zero.
//
// Container resource limits are deliberately conservative (32Mi memory,
// 50m CPU) — the script is a curl + hash, not a workload.
func BuildPluginVerifiedDownloadInitContainer(
	plugin *neo4jv1beta1.Neo4jPlugin,
	image string,
	caSecrets []neo4jv1beta1.TrustedCASecret,
) corev1.Container {
	if image == "" {
		image = DefaultPluginInitContainerImage
	}
	if plugin.Spec.Source == nil {
		// Defensive: the validator rejects VerifiedDownload without
		// source, so this shouldn't fire. Returning a benign container
		// instead of panicking keeps the operator resilient if the
		// validator is bypassed (e.g. spec applied via raw apiserver).
		return corev1.Container{Name: PluginInitContainerName(plugin.Spec.Name), Image: image}
	}

	envVars := []corev1.EnvVar{
		{Name: "PLUGIN_NAME", Value: plugin.Spec.Name},
		{Name: "PLUGIN_URL", Value: plugin.Spec.Source.URL},
		{Name: "PLUGIN_CHECKSUM", Value: plugin.Spec.Source.Checksum},
		// Target file lives under /plugins/<name>.jar. The Neo4j
		// entrypoint loads anything matching /plugins/*.jar, so the
		// filename convention matches what a manual install would
		// produce.
		{Name: "PLUGIN_TARGET", Value: "/plugins/" + plugin.Spec.Name + ".jar"},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: pluginsVolumeName, MountPath: "/plugins"},
	}

	// Auth secret: if spec.source.authSecret is set, the caller wires
	// the Secret as a projected volume at /etc/plugin-auth. We expect
	// the Secret to carry either a `token` key (used as a Bearer
	// header) or `header` (full Authorization header value). The
	// script below probes for both in priority order.
	if plugin.Spec.Source.AuthSecret != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      pluginAuthVolumeName(plugin.Spec.Name),
			MountPath: "/etc/plugin-auth",
			ReadOnly:  true,
		})
	}

	// CA bundle: concatenate every TrustedCASecret's `ca.crt` (or the
	// per-secret Key override) into a single file at runtime via the
	// script — projection into /etc/plugin-ca/<secret>.crt. curl is
	// then pointed at the concatenation.
	if len(caSecrets) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      pluginCAVolumeName,
			MountPath: "/etc/plugin-ca",
			ReadOnly:  true,
		})
	}

	return corev1.Container{
		Name:            PluginInitContainerName(plugin.Spec.Name),
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars,
		Command:         []string{"/bin/sh"},
		Args:            []string{"-c", pluginInitScript(plugin.Spec.Source.AuthSecret != "", len(caSecrets) > 0)},
		VolumeMounts:    volumeMounts,
		SecurityContext: DefaultNeo4jContainerSecurityContext(),
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
	}
}

// BuildPluginAuthVolume returns the projected Secret volume that
// surfaces the authSecret to the init container. The caller is the
// plugin controller — when merging the init container into the
// StatefulSet PodSpec it must also append this volume.
func BuildPluginAuthVolume(plugin *neo4jv1beta1.Neo4jPlugin) *corev1.Volume {
	if plugin.Spec.Source == nil || plugin.Spec.Source.AuthSecret == "" {
		return nil
	}
	return &corev1.Volume{
		Name: pluginAuthVolumeName(plugin.Spec.Name),
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: plugin.Spec.Source.AuthSecret,
			},
		},
	}
}

// BuildPluginCAVolume returns the projected volume that fans the
// cluster's TrustedCASecrets into /etc/plugin-ca/<secret>.crt. nil if
// no secrets are configured. Shared across all VerifiedDownload
// plugins on the same cluster — only one volume per pod.
func BuildPluginCAVolume(caSecrets []neo4jv1beta1.TrustedCASecret) *corev1.Volume {
	if len(caSecrets) == 0 {
		return nil
	}
	sources := make([]corev1.VolumeProjection, 0, len(caSecrets))
	for _, s := range caSecrets {
		key := s.Key
		if key == "" {
			key = "ca.crt"
		}
		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: s.Name},
				Items:                []corev1.KeyToPath{{Key: key, Path: s.Name + ".crt"}},
			},
		})
	}
	return &corev1.Volume{
		Name: pluginCAVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{Sources: sources},
		},
	}
}

// pluginInitScript is the /bin/sh body the init container executes.
// withAuth and withCA gate the optional steps so the script remains
// minimal in the common case (public URL, system CA).
//
// Failure semantics: any non-zero exit aborts. The pod stays Pending
// with the init container's terminated reason visible via
// `kubectl describe pod`; the plugin reconciler surfaces it into
// Neo4jPlugin.status.message on the next reconcile.
func pluginInitScript(withAuth, withCA bool) string {
	var b strings.Builder
	b.WriteString(`set -eu
echo "[plugin-init] starting download for plugin=${PLUGIN_NAME} target=${PLUGIN_TARGET}"
`)

	// Build curl invocation. --fail makes HTTP errors non-zero,
	// --location follows redirects (most plugin registries do), and
	// --silent keeps logs readable while --show-error still prints on
	// failure.
	b.WriteString("CURL_ARGS=\"--fail --location --silent --show-error\"\n")

	if withCA {
		// Concatenate every certificate file in /etc/plugin-ca into a
		// single bundle; curl accepts only one --cacert path. The
		// projected volume drops files as <secret-name>.crt.
		b.WriteString(`mkdir -p /tmp/plugin-init
cat /etc/plugin-ca/*.crt > /tmp/plugin-init/ca-bundle.crt
CURL_ARGS="${CURL_ARGS} --cacert /tmp/plugin-init/ca-bundle.crt"
echo "[plugin-init] using custom CA bundle"
`)
	}

	if withAuth {
		// Auth Secret may carry one of two keys:
		//   * token  — used as `Authorization: Bearer <token>`
		//   * header — used verbatim as `Authorization: <value>`
		// Probe in order. Either is enough; missing both is a hard
		// error since the user explicitly opted into authSecret.
		b.WriteString(`if [ -f /etc/plugin-auth/header ]; then
    AUTH_HEADER="Authorization: $(cat /etc/plugin-auth/header)"
elif [ -f /etc/plugin-auth/token ]; then
    AUTH_HEADER="Authorization: Bearer $(cat /etc/plugin-auth/token)"
else
    echo "[plugin-init] ERROR: authSecret mounted but neither 'header' nor 'token' key present" >&2
    exit 1
fi
CURL_ARGS="${CURL_ARGS} --header"
echo "[plugin-init] using authenticated download"
`)
		// The actual curl invocation handles the header as a separate
		// arg so the value can contain spaces safely.
		b.WriteString(`curl ${CURL_ARGS} "${AUTH_HEADER}" -o "${PLUGIN_TARGET}" "${PLUGIN_URL}"
`)
	} else {
		b.WriteString(`curl ${CURL_ARGS} -o "${PLUGIN_TARGET}" "${PLUGIN_URL}"
`)
	}

	// Verify checksum. Prefix decides algorithm — validator enforced
	// sha256:|sha512:.
	b.WriteString(`case "${PLUGIN_CHECKSUM}" in
    sha256:*) ALGO="sha256sum"; EXPECTED="${PLUGIN_CHECKSUM#sha256:}" ;;
    sha512:*) ALGO="sha512sum"; EXPECTED="${PLUGIN_CHECKSUM#sha512:}" ;;
    *) echo "[plugin-init] ERROR: unsupported checksum prefix in ${PLUGIN_CHECKSUM}" >&2; exit 1 ;;
esac
ACTUAL=$(${ALGO} "${PLUGIN_TARGET}" | awk '{print $1}')
if [ "${ACTUAL}" != "${EXPECTED}" ]; then
    echo "[plugin-init] ERROR: checksum mismatch for ${PLUGIN_NAME}" >&2
    echo "[plugin-init]   expected: ${EXPECTED}" >&2
    echo "[plugin-init]   actual:   ${ACTUAL}" >&2
    rm -f "${PLUGIN_TARGET}"
    exit 1
fi
echo "[plugin-init] verified ${PLUGIN_NAME} (${ALGO} ok)"
`)

	return b.String()
}

// Volume / mount names shared across the helpers. Constants so the
// caller (plugin controller) can append the matching Volume to the
// StatefulSet without re-deriving the name.
const (
	pluginsVolumeName  = "plugins"
	pluginCAVolumeName = "plugin-ca"
)

func pluginAuthVolumeName(pluginName string) string {
	// Volume names share the DNS-1123 namespace; the prefix keeps
	// them obviously plugin-owned to the operator.
	return "plugin-auth-" + pluginName
}
