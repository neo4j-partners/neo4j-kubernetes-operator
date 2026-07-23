package workload

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/plugins"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

const (
	neo4jConfVolumeName = "neo4j-conf"
	apocConfVolumeName  = "apoc-conf"
	// configVolumeDefaultMode matches Helm (neo4j-statefulset.yaml defaultMode: 0440).
	// Neo4j --expand-commands rejects config files readable by others (default ConfigMap mode 0644).
	configVolumeDefaultMode int32 = 0o440
)

// PoolStatefulSet builds a StatefulSet for one workload pool (Standalone or Cluster).
func PoolStatefulSet(ctx render.Context) *appsv1.StatefulSet {
	replicas := ctx.PoolReplicas()
	labels := ctx.WorkloadLabels()
	pullPolicy := corev1.PullIfNotPresent
	if ctx.Neo4j.Spec.Image != nil && ctx.Neo4j.Spec.Image.PullPolicy != "" {
		pullPolicy = ctx.Neo4j.Spec.Image.PullPolicy
	}

	configMode := configVolumeDefaultMode
	container := corev1.Container{
		Name:            "neo4j",
		Image:           ctx.ImageRef(),
		ImagePullPolicy: pullPolicy,
		Ports:           neo4jContainerPorts(ctx),
		Env:             neo4jContainerEnv(ctx),
		SecurityContext: defaultContainerSecurityContext(),
		VolumeMounts: []corev1.VolumeMount{
			// Helm mounts projected config fragments at /config/neo4j.conf (directory).
			{Name: neo4jConfVolumeName, MountPath: "/config/neo4j.conf"},
		},
	}
	applyProbes(ctx, &container)
	volumes := []corev1.Volume{neo4jConfVolume(ctx, configMode)}
	if rendercfg.HasApocConfig(ctx) {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: apocConfVolumeName, MountPath: "/config/",
		})
		volumes = append(volumes, apocConfVolume(ctx, configMode))
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: ctx.OperandServiceAccountName(),
		SecurityContext:    defaultPodSecurityContext(),
		Containers:         []corev1.Container{container},
		Volumes:            volumes,
		ImagePullSecrets:   imagePullSecrets(ctx),
	}
	storageVCTs := renderstorage.Apply(ctx, &podSpec.Containers[0], &podSpec)
	appendPluginLicenseVolumes(ctx, &podSpec.Containers[0], &podSpec)
	rendertrust.AppendVolumes(ctx, &podSpec.Containers[0], &podSpec)
	applyScheduling(ctx, &podSpec)
	// After scheduling so offline can force terminationGracePeriodSeconds=0.
	applyOfflineMaintenance(ctx, &podSpec.Containers[0], &podSpec)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.STSName(),
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			// Cluster formation needs all ordinals up together; OrderedReady deadlocks when
			// Neo4j waits for quorum before Bolt (and thus readiness) comes up.
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector:            &metav1.LabelSelector{MatchLabels: ctx.SelectorLabels()},
			ServiceName:         ctx.HeadlessServiceName(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						rendercfg.ConfigChecksumAnnotation: rendercfg.ConfigChecksum(ctx),
					},
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: storageVCTs,
		},
	}
}

func neo4jConfVolume(ctx render.Context, mode int32) corev1.Volume {
	return corev1.Volume{
		Name: neo4jConfVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &mode,
				Sources: []corev1.VolumeProjection{
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: ctx.ConfigMapName()},
						},
					},
				},
			},
		},
	}
}

func apocConfVolume(ctx render.Context, mode int32) corev1.Volume {
	return corev1.Volume{
		Name: apocConfVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &mode,
				Sources: []corev1.VolumeProjection{
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: ctx.ApocConfigMapName()},
						},
					},
				},
			},
		},
	}
}

func defaultPodSecurityContext() *corev1.PodSecurityContext {
	runAsNonRoot := true
	runAsUser := int64(7474)
	runAsGroup := int64(7474)
	fsGroup := int64(7474)
	policy := corev1.FSGroupChangeAlways
	return &corev1.PodSecurityContext{
		RunAsNonRoot:        &runAsNonRoot,
		RunAsUser:           &runAsUser,
		RunAsGroup:          &runAsGroup,
		FSGroup:             &fsGroup,
		FSGroupChangePolicy: &policy,
	}
}

func defaultContainerSecurityContext() *corev1.SecurityContext {
	runAsNonRoot := true
	runAsUser := int64(7474)
	runAsGroup := int64(7474)
	return &corev1.SecurityContext{
		RunAsNonRoot: &runAsNonRoot,
		RunAsUser:    &runAsUser,
		RunAsGroup:   &runAsGroup,
		Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

// StandaloneStatefulSet is an alias kept for tests; Standalone uses the server pool.
func StandaloneStatefulSet(ctx render.Context) *appsv1.StatefulSet {
	return PoolStatefulSet(ctx)
}

func neo4jContainerPorts(ctx render.Context) []corev1.ContainerPort {
	ports := make([]corev1.ContainerPort, 0, 8)
	if ctx.HTTPEnabled() {
		ports = append(ports, corev1.ContainerPort{Name: "http", ContainerPort: ctx.HTTPPort()})
	}
	if ctx.BoltEnabled() {
		ports = append(ports, corev1.ContainerPort{Name: "bolt", ContainerPort: ctx.BoltPort()})
	}
	if ctx.HTTPSEnabled() {
		ports = append(ports, corev1.ContainerPort{Name: "https", ContainerPort: ctx.HTTPSPort()})
	}
	if ctx.BackupListenerEnabled() {
		ports = append(ports, corev1.ContainerPort{Name: "backup", ContainerPort: ctx.BackupPort()})
	}
	if ctx.MetricsListenerEnabled() {
		ports = append(ports, corev1.ContainerPort{Name: "tcp-prometheus", ContainerPort: ctx.MetricsPort()})
	}
	if render.IsClusterMode(ctx.Neo4j) {
		ports = append(ports,
			corev1.ContainerPort{Name: "tcp-boltrouting", ContainerPort: 7688},
			corev1.ContainerPort{Name: "tcp-discovery", ContainerPort: 5000},
			corev1.ContainerPort{Name: "tcp-tx", ContainerPort: 6000},
			corev1.ContainerPort{Name: "tcp-raft", ContainerPort: 7000},
		)
	}
	return ports
}

func neo4jContainerEnv(ctx render.Context) []corev1.EnvVar {
	checksum := rendercfg.ConfigChecksum(ctx)
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
			Value: ctx.LicenseAcceptEnv(),
		},
		{
			Name:  "NEO4J_EDITION",
			Value: ctx.Neo4jEditionK8SEnv(),
		},
		{
			Name:  "NEO4J_CONF",
			Value: "/config/",
		},
		{
			Name:  "EXTENDED_CONF",
			Value: "yes",
		},
		{
			Name:  "K8S_NEO4J_NAME",
			Value: render.AppNameValue,
		},
		{
			Name:  rendercfg.ConfigChecksumEnv,
			Value: checksum,
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
	if render.IsClusterMode(ctx.Neo4j) {
		// Helm parity: SERVICE_NEO4J / SERVICE_NEO4J_INTERNALS are per-member Service FQDNs.
		// K8s expands $(POD_NAME)/$(NAMESPACE) from prior env entries (fieldRef + static).
		env = append(env,
			corev1.EnvVar{
				Name: "NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			corev1.EnvVar{Name: "CLUSTER_DOMAIN", Value: ctx.ClusterDomain()},
			corev1.EnvVar{
				Name:  "SERVICE_NEO4J",
				Value: "$(POD_NAME).$(NAMESPACE).svc.$(CLUSTER_DOMAIN)",
			},
			corev1.EnvVar{
				Name:  "SERVICE_NEO4J_INTERNALS",
				Value: "$(POD_NAME)-internals.$(NAMESPACE).svc.$(CLUSTER_DOMAIN)",
			},
		)
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

// imagePullSecrets maps spec.image.pullSecrets → pod ImagePullSecrets (NEO-3-004-IMG-01).
func imagePullSecrets(ctx render.Context) []corev1.LocalObjectReference {
	if ctx.Neo4j.Spec.Image == nil || len(ctx.Neo4j.Spec.Image.PullSecrets) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, 0, len(ctx.Neo4j.Spec.Image.PullSecrets))
	for _, name := range ctx.Neo4j.Spec.Image.PullSecrets {
		if name == "" {
			continue
		}
		out = append(out, corev1.LocalObjectReference{Name: name})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// OperandServiceAccount builds the workload ServiceAccount.
func OperandServiceAccount(ctx render.Context) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.OperandServiceAccountName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("workload"),
		},
	}
	if ctx.Neo4j.Spec.Security != nil && ctx.Neo4j.Spec.Security.ServiceAccount != nil &&
		len(ctx.Neo4j.Spec.Security.ServiceAccount.Annotations) > 0 {
		sa.Annotations = maps.Clone(ctx.Neo4j.Spec.Security.ServiceAccount.Annotations)
	}
	return sa
}
