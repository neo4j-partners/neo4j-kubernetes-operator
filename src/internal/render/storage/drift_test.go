package storage

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func vct(name, size, class string, modes ...corev1.PersistentVolumeAccessMode) corev1.PersistentVolumeClaim {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
			AccessModes: modes,
		},
	}
	if class != "" {
		c := class
		pvc.Spec.StorageClassName = &c
	}
	return pvc
}

func TestVolumeClaimDrift(t *testing.T) {
	cases := []struct {
		name    string
		desired []corev1.PersistentVolumeClaim
		live    []corev1.PersistentVolumeClaim
		want    string // substring expected in the report; "" means no drift
	}{
		{
			name:    "identical templates",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
		},
		{
			// The whole point: after a grow the claims carry 10Gi and the immutable template still
			// says 5Gi. Reporting that as drift would refuse every update forever.
			name:    "size differs after a grow",
			desired: []corev1.PersistentVolumeClaim{vct("data", "10Gi", "standard")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
		},
		{
			name:    "a volume the StatefulSet has no template for",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard"), vct("backups", "5Gi", "standard")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
			want:    `asks for volume "backups"`,
		},
		{
			name:    "a template the spec no longer asks for",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard"), vct("logs", "5Gi", "standard")},
			want:    `no longer asks for`,
		},
		{
			name:    "StorageClass changed",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "fast")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
			want:    `asks for StorageClass "fast"`,
		},
		{
			// The API server fills the class in on the live template. Comparing an unset rendered
			// class against it would report drift on every pass of an aligned StatefulSet.
			name:    "rendered class unset, live class defaulted",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "")},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard")},
		},
		{
			name:    "access modes changed",
			desired: []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard", corev1.ReadWriteMany)},
			live:    []corev1.PersistentVolumeClaim{vct("data", "5Gi", "standard", corev1.ReadWriteOnce)},
			want:    "access modes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := VolumeClaimDrift(tc.desired, tc.live)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("VolumeClaimDrift() = %q, want no drift", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("VolumeClaimDrift() = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}
