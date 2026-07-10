package workload

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/plugins"
	rendercfg "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/serverconfig"
)

const (
	dataVolumeName   = "data"
	configVolumeName = "config"
)

// StandaloneStatefulSet builds a one-replica StatefulSet for Standalone mode (BDR-002).
func StandaloneStatefulSet(ctx render.Context) *appsv1.StatefulSet {
	replicas := int32(1)
	labels := ctx.WorkloadLabels()
	pullPolicy := corev1.PullIfNotPresent
	if ctx.Neo4j.Spec.Image != nil && ctx.Neo4j.Spec.Image.PullPolicy != "" {
		pullPolicy = ctx.Neo4j.Spec.Image.PullPolicy
	}

	vct := volumeClaimTemplate(ctx)
	podSpec := corev1.PodSpec{
		ServiceAccountName: ctx.OperandServiceAccountName(),
		Containers: []corev1.Container{
			{
				Name:            "neo4j",
				Image:           ctx.ImageRef(),
				ImagePullPolicy: pullPolicy,
				Ports: []corev1.ContainerPort{
					{Name: "bolt", ContainerPort: ctx.BoltPort()},
					{Name: "http", ContainerPort: ctx.HTTPPort()},
				},
				Env: neo4jContainerEnv(ctx),
				VolumeMounts: []corev1.VolumeMount{
					{Name: dataVolumeName, MountPath: "/data"},
					{Name: configVolumeName, MountPath: "/config"},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: ctx.ConfigMapName()},
					},
				},
			},
		},
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.STSName(),
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: ctx.SelectorLabels()},
			ServiceName: ctx.HeadlessServiceName(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						rendercfg.ConfigChecksumAnnotation: rendercfg.ConfigChecksum(ctx),
					},
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{vct},
		},
	}
}

func neo4jContainerEnv(ctx render.Context) []corev1.EnvVar {
	checksum := rendercfg.ConfigChecksum(ctx)
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
			Value: ctx.LicenseAcceptEnv(),
		},
		{
			Name:  "NEO4J_CONF",
			Value: "/config",
		},
		{
			Name:  rendercfg.ConfigChecksumEnv,
			Value: checksum,
		},
	}
	if pluginsEnv := plugins.NEO4JPluginsEnv(ctx.PoolPluginIDs()); pluginsEnv != "" {
		env = append(env, corev1.EnvVar{
			Name:  "NEO4J_PLUGINS",
			Value: pluginsEnv,
		})
	}
	env = append(env, corev1.EnvVar{
		Name: "NEO4J_AUTH",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: ctx.AuthSecretName()},
				Key:                  "NEO4J_AUTH",
			},
		},
	})
	return env
}

func volumeClaimTemplate(ctx render.Context) corev1.PersistentVolumeClaim {
	size := ctx.DataVolumeSize()
	storageClass := ctx.DataStorageClassName()
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	if ctx.Neo4j.Spec.Storage != nil && ctx.Neo4j.Spec.Storage.Volumes != nil &&
		ctx.Neo4j.Spec.Storage.Volumes.Data.Dynamic != nil &&
		ctx.Neo4j.Spec.Storage.Volumes.Data.Dynamic.AccessMode != "" {
		accessModes = []corev1.PersistentVolumeAccessMode{
			corev1.PersistentVolumeAccessMode(ctx.Neo4j.Spec.Storage.Volumes.Data.Dynamic.AccessMode),
		}
	}

	spec := corev1.PersistentVolumeClaimSpec{
		AccessModes: accessModes,
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(size),
			},
		},
	}
	if storageClass != "" {
		spec.StorageClassName = &storageClass
	}

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: dataVolumeName},
		Spec:       spec,
	}
}

// OperandServiceAccount builds the workload ServiceAccount.
func OperandServiceAccount(ctx render.Context) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.OperandServiceAccountName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("workload"),
		},
	}
}

// IsStandalone returns true when topology mode is Standalone.
func IsStandalone(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Topology.Mode == neo4jv1beta1.TopologyModeStandalone
}
