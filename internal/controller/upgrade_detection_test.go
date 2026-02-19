package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// TestValidateVersionCompatibility covers the version-check logic in the rolling
// upgrade orchestrator, including the fix that allows an empty Status.Version so
// that the first upgrade after a fresh operator deployment is not blocked.
func TestValidateVersionCompatibility(t *testing.T) {
	orch := &RollingUpgradeOrchestrator{}

	cases := []struct {
		name        string
		current     string
		target      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "empty current version is allowed (first upgrade)",
			current: "", target: "5.27.0-enterprise",
			wantErr: false,
		},
		{
			name:    "valid SemVer patch upgrade",
			current: "5.26.0-enterprise", target: "5.26.3-enterprise",
			wantErr: false,
		},
		{
			name:    "valid SemVer minor upgrade",
			current: "5.26.0-enterprise", target: "5.27.0-enterprise",
			wantErr: false,
		},
		{
			name:    "valid CalVer upgrade",
			current: "2025.01.0-enterprise", target: "2025.02.0-enterprise",
			wantErr: false,
		},
		{
			name:    "SemVer to CalVer upgrade is allowed",
			current: "5.26.0-enterprise", target: "2025.01.0-enterprise",
			wantErr: false,
		},
		{
			name:    "SemVer downgrade is rejected",
			current: "5.27.0-enterprise", target: "5.26.0-enterprise",
			wantErr:     true,
			errContains: "downgrade",
		},
		{
			name:    "CalVer to SemVer downgrade is rejected",
			current: "2025.01.0-enterprise", target: "5.27.0-enterprise",
			wantErr:     true,
			errContains: "downgrade",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := orch.validateVersionCompatibility(tc.current, tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for current=%q target=%q, got nil", tc.current, tc.target)
				}
				if tc.errContains != "" {
					msg := err.Error()
					found := false
					for _, seg := range []string{tc.errContains} {
						if len(msg) > 0 && containsIgnoreCase(msg, seg) {
							found = true
						}
					}
					if !found {
						t.Fatalf("expected error to contain %q, got: %v", tc.errContains, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for current=%q target=%q: %v", tc.current, tc.target, err)
				}
			}
		})
	}
}

func containsIgnoreCase(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			ls, lsub := len(s), len(sub)
			for i := 0; i <= ls-lsub; i++ {
				match := true
				for j := 0; j < lsub; j++ {
					cs, csub := s[i+j], sub[j]
					if cs >= 'A' && cs <= 'Z' {
						cs += 32
					}
					if csub >= 'A' && csub <= 'Z' {
						csub += 32
					}
					if cs != csub {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
			return false
		}())
}

func TestIsUpgradeRequiredSingleStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.27.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
		},
		Status: neo4jv1alpha1.Neo4jEnterpriseClusterStatus{
			Phase: "Ready",
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-server",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: pointer.Int32(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "neo4j",
							Image: "neo4j:5.26.0-enterprise",
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 3,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	reconciler := &Neo4jEnterpriseClusterReconciler{Client: fakeClient}

	if !reconciler.isUpgradeRequired(context.Background(), cluster) {
		t.Fatalf("expected upgrade to be required when StatefulSet image differs")
	}

	existing := &appsv1.StatefulSet{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "demo-server",
		Namespace: "default",
	}, existing); err != nil {
		t.Fatalf("failed to fetch StatefulSet: %v", err)
	}
	existing.Spec.Template.Spec.Containers[0].Image = "neo4j:5.27.0-enterprise"
	if err := fakeClient.Update(context.Background(), existing); err != nil {
		t.Fatalf("failed to update StatefulSet: %v", err)
	}

	if reconciler.isUpgradeRequired(context.Background(), cluster) {
		t.Fatalf("expected upgrade not to be required when images match")
	}
}
