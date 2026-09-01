package storage

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const dataVolumeName = "data"

// BackupsMountPath and BackupsSubPath define where the backups volume is mounted inside the
// Neo4j workload and the sub-directory it exposes within the claim. They are the shared contract
// for the PVC backup→restore round-trip (ADR-015): the backup Job must write under the same
// sub-path, and restore must read file:BackupsMountPath/<artifact>. Exported so render/backup and
// the restore controller reference one source of truth instead of duplicating the literals — a
// mismatch there silently breaks the round-trip ("seed not found").
const (
	BackupsMountPath = "/backups"
	BackupsSubPath   = "backups"
)

type auxRole struct {
	name      string
	mountPath string
	spec      *neo4jv1beta1.AuxiliaryVolumeSpec
}

// Apply attaches data + aux + escape-hatch mounts/volumes and returns StatefulSet volumeClaimTemplates.
func Apply(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) []corev1.PersistentVolumeClaim {
	if ctx.Neo4j.Spec.Storage == nil || ctx.Neo4j.Spec.Storage.Volumes == nil {
		return nil
	}
	vols := ctx.Neo4j.Spec.Storage.Volumes
	var vcts []corev1.PersistentVolumeClaim

	dataVCT, dataVol := materializeData(ctx, &vols.Data)
	if dataVCT != nil {
		vcts = append(vcts, *dataVCT)
	}
	if dataVol != nil {
		podSpec.Volumes = append(podSpec.Volumes, *dataVol)
	}
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      dataVolumeName,
		MountPath: "/data",
	})

	for _, role := range auxRoles(vols) {
		if role.spec == nil {
			continue
		}
		vct, vol, mount := materializeAux(ctx, role)
		if vct != nil {
			vcts = append(vcts, *vct)
		}
		if vol != nil {
			podSpec.Volumes = append(podSpec.Volumes, *vol)
		}
		if mount != nil {
			container.VolumeMounts = append(container.VolumeMounts, *mount)
		}
	}

	appendAdditionalMounts(ctx, container, podSpec)
	appendSecretMounts(ctx, container, podSpec)
	return vcts
}

// VolumeClaimTemplates returns just the claims a pool's StatefulSet is rendered with, without the
// mounts Apply also produces. PVC expansion and the drift guard both need the desired claims on
// their own, and neither has a container to hand (BDR-005).
func VolumeClaimTemplates(ctx render.Context) []corev1.PersistentVolumeClaim {
	var container corev1.Container
	var podSpec corev1.PodSpec
	return Apply(ctx, &container, &podSpec)
}

// ClaimName is the PVC a StatefulSet creates for one volume and one ordinal. Kubernetes derives it
// as {volume}-{statefulset}-{ordinal} and the operator has to reconstruct it to grow claims: the
// StatefulSet exposes no list of the claims it owns.
func ClaimName(volume, stsName string, ordinal int32) string {
	return fmt.Sprintf("%s-%s-%d", volume, stsName, ordinal)
}

// DataPVCLookup returns the PVC name to observe for StorageReady, if any.
// ok=false means readiness cannot be inferred from a PVC (e.g. raw Volume).
func DataPVCLookup(ctx render.Context) (name string, ok bool) {
	if ctx.Neo4j.Spec.Storage == nil || ctx.Neo4j.Spec.Storage.Volumes == nil {
		return "", false
	}
	data := ctx.Neo4j.Spec.Storage.Volumes.Data
	switch data.Mode {
	case neo4jv1beta1.VolumeModeDynamic:
		return fmt.Sprintf("%s-%s-0", dataVolumeName, ctx.STSName()), true
	case neo4jv1beta1.VolumeModeExisting:
		if data.Existing == nil {
			return "", false
		}
		if data.Existing.ClaimName != "" {
			return data.Existing.ClaimName, true
		}
		if data.Existing.VolumeClaimTemplate != nil {
			return fmt.Sprintf("%s-%s-0", dataVolumeName, ctx.STSName()), true
		}
		return "", false
	default:
		return "", false
	}
}

func auxRoles(vols *neo4jv1beta1.VolumesSpec) []auxRole {
	return []auxRole{
		{name: "backups", mountPath: BackupsMountPath, spec: vols.Backups},
		{name: "logs", mountPath: "/logs", spec: vols.Logs},
		{name: "metrics", mountPath: "/metrics", spec: vols.Metrics},
		{name: "import", mountPath: "/import", spec: vols.Import},
		{name: "licenses", mountPath: "/licenses", spec: vols.Licenses},
		{name: "plugins", mountPath: "/plugins", spec: vols.Plugins},
	}
}

func materializeData(ctx render.Context, data *neo4jv1beta1.DataVolumeSpec) (*corev1.PersistentVolumeClaim, *corev1.Volume) {
	switch data.Mode {
	case neo4jv1beta1.VolumeModeDynamic:
		return dynamicPVC(ctx, dataVolumeName, data.Dynamic), nil
	case neo4jv1beta1.VolumeModeExisting:
		return existingMaterial(dataVolumeName, data.Existing)
	default:
		return nil, nil
	}
}

func materializeAux(ctx render.Context, role auxRole) (*corev1.PersistentVolumeClaim, *corev1.Volume, *corev1.VolumeMount) {
	mode := role.spec.Mode
	if mode == "" {
		mode = neo4jv1beta1.VolumeModeShare
	}
	switch mode {
	case neo4jv1beta1.VolumeModeShare:
		mount := &corev1.VolumeMount{
			Name:        dataVolumeName,
			MountPath:   role.mountPath,
			SubPathExpr: shareSubPathExpr(role.name),
		}
		return nil, nil, mount
	case neo4jv1beta1.VolumeModeDynamic:
		vct := dynamicPVC(ctx, role.name, role.spec.Dynamic)
		mount := &corev1.VolumeMount{Name: role.name, MountPath: role.mountPath, SubPathExpr: shareSubPathExpr(role.name)}
		return vct, nil, mount
	case neo4jv1beta1.VolumeModeExisting:
		vct, vol := existingMaterial(role.name, role.spec.Existing)
		mount := &corev1.VolumeMount{Name: role.name, MountPath: role.mountPath, SubPathExpr: shareSubPathExpr(role.name)}
		if vct == nil && vol == nil {
			return nil, nil, nil
		}
		return vct, vol, mount
	default:
		return nil, nil, nil
	}
}

func shareSubPathExpr(role string) string {
	switch role {
	case "logs", "metrics":
		return role + "/$(POD_NAME)"
	case "backups":
		// Pinned to the exported round-trip contract (equals the role name, but stated
		// explicitly so the backup Job / restore coupling is not an accident).
		return BackupsSubPath
	default:
		return role
	}
}

func dynamicPVC(ctx render.Context, name string, dyn *neo4jv1beta1.DynamicVolumeSpec) *corev1.PersistentVolumeClaim {
	if dyn == nil {
		return nil
	}
	size := dyn.Size
	if size == "" {
		size = "10Gi"
	}
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	if dyn.AccessMode != "" {
		accessModes = []corev1.PersistentVolumeAccessMode{
			corev1.PersistentVolumeAccessMode(dyn.AccessMode),
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
	if dyn.StorageClassName != "" {
		sc := dyn.StorageClassName
		spec.StorageClassName = &sc
	}
	meta := metav1.ObjectMeta{Name: name}
	labels := map[string]string{}
	for k, v := range dyn.Labels {
		labels[k] = v
	}
	// Operator identity labels win over user dynamic.labels (NEO-015).
	for k, v := range ctx.CommonLabels("storage") {
		labels[k] = v
	}
	meta.Labels = labels
	return &corev1.PersistentVolumeClaim{ObjectMeta: meta, Spec: spec}
}

func existingMaterial(name string, existing *neo4jv1beta1.ExistingVolumeSpec) (*corev1.PersistentVolumeClaim, *corev1.Volume) {
	if existing == nil {
		return nil, nil
	}
	switch {
	case existing.ClaimName != "":
		return nil, &corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: existing.ClaimName,
				},
			},
		}
	case existing.Volume != nil:
		vol := existing.Volume.DeepCopy()
		vol.Name = name
		return nil, vol
	case existing.VolumeClaimTemplate != nil:
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       *existing.VolumeClaimTemplate.DeepCopy(),
		}, nil
	default:
		return nil, nil
	}
}

func appendAdditionalMounts(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if ctx.Neo4j.Spec.Storage == nil {
		return
	}
	for _, m := range ctx.Neo4j.Spec.Storage.AdditionalMounts {
		vol := m.Volume
		vol.Name = m.Name
		podSpec.Volumes = append(podSpec.Volumes, vol)
		mount := corev1.VolumeMount{
			Name:      m.Name,
			MountPath: m.MountPath,
			SubPath:   m.SubPath,
			ReadOnly:  m.ReadOnly,
		}
		container.VolumeMounts = append(container.VolumeMounts, mount)
	}
}

func appendSecretMounts(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if ctx.Neo4j.Spec.Storage == nil || len(ctx.Neo4j.Spec.Storage.SecretMounts) == 0 {
		return
	}
	mode440 := int32(0o440)
	// Stable order for tests / diffs.
	names := make([]string, 0, len(ctx.Neo4j.Spec.Storage.SecretMounts))
	for name := range ctx.Neo4j.Spec.Storage.SecretMounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sm := ctx.Neo4j.Spec.Storage.SecretMounts[name]
		volName := "secret-" + name
		secretName := sm.SecretName
		if secretName == "" {
			secretName = name
		}
		proj := &corev1.SecretVolumeSource{SecretName: secretName}
		if sm.DefaultMode != nil {
			proj.DefaultMode = sm.DefaultMode
		} else {
			proj.DefaultMode = &mode440
		}
		for _, item := range sm.Items {
			proj.Items = append(proj.Items, corev1.KeyToPath{Key: item.Key, Path: item.Path})
		}
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name:         volName,
			VolumeSource: corev1.VolumeSource{Secret: proj},
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: sm.MountPath,
			ReadOnly:  true,
		})
	}
}
