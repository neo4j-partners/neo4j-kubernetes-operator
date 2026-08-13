package workload

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const pluginsVolumeName = "plugins"

// ensurePluginsMount makes /plugins writable for NEO4J_PLUGINS when catalog plugins
// are assigned but the CR did not set storage.volumes.plugins. Without a mount, the
// image entrypoint installs jars under $NEO4J_HOME/plugins while we point Neo4j at
// /plugins — emptyDir aligns both sides. Explicit volumes.plugins (Share/Dynamic/…)
// already mounts /plugins via storage.Apply.
func ensurePluginsMount(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if len(ctx.PoolPluginIDs()) == 0 {
		return
	}
	if ctx.Neo4j.Spec.Storage != nil && ctx.Neo4j.Spec.Storage.Volumes != nil &&
		ctx.Neo4j.Spec.Storage.Volumes.Plugins != nil {
		return
	}
	for _, m := range container.VolumeMounts {
		if m.MountPath == "/plugins" {
			return
		}
	}
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: pluginsVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      pluginsVolumeName,
		MountPath: "/plugins",
	})
}

func appendPluginLicenseVolumes(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if ctx.Neo4j.Spec.PluginDefinitions == nil {
		return
	}
	for _, pluginID := range ctx.PoolPluginIDs() {
		def, ok := ctx.Neo4j.Spec.PluginDefinitions[pluginID]
		if !ok || def.LicenseSecretRef == "" {
			continue
		}
		volName := "license-" + pluginID
		mountPath := "/licenses/" + pluginID
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: def.LicenseSecretRef,
				},
			},
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}
}
