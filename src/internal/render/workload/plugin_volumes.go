package workload

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

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
