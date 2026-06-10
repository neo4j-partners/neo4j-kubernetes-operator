/*
Copyright 2025.

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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

// TestApplyDesiredServiceFields_PreservesImmutablesAndAppliesChanges pins the
// field-by-field Service update: a ClusterIP→LoadBalancer change with new
// annotations is applied, while the API-assigned ClusterIP/ClusterIPs/
// HealthCheckNodePort and an allocated NodePort are preserved (so the Update
// is not rejected on an immutable field).
func TestApplyDesiredServiceFields_PreservesImmutablesAndAppliesChanges(t *testing.T) {
	existing := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:       corev1.ServiceTypeClusterIP,
			ClusterIP:  "10.0.0.5",
			ClusterIPs: []string{"10.0.0.5"},
			IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			Selector:   map[string]string{"app": "neo4j"},
			Ports:      []corev1.ServicePort{{Name: "bolt", Port: 7687, NodePort: 31687}},
		},
	}
	existing.Annotations = map[string]string{"foreign.io/keep": "1"}

	desired := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{"app": "neo4j"},
			// NodePort intentionally unset — must inherit the allocated 31687.
			Ports: []corev1.ServicePort{{Name: "bolt", Port: 7687}},
		},
	}
	desired.Annotations = map[string]string{"service.beta.kubernetes.io/aws-load-balancer-type": "nlb"}

	changed := applyDesiredServiceFields(existing, desired)
	assert.True(t, changed, "type + annotation change must be detected")

	assert.Equal(t, corev1.ServiceTypeLoadBalancer, existing.Spec.Type, "type change applied")
	assert.Equal(t, "10.0.0.5", existing.Spec.ClusterIP, "ClusterIP preserved (immutable)")
	assert.Equal(t, []string{"10.0.0.5"}, existing.Spec.ClusterIPs, "ClusterIPs preserved (immutable)")
	assert.Equal(t, []corev1.IPFamily{corev1.IPv4Protocol}, existing.Spec.IPFamilies, "IPFamilies preserved")
	assert.Equal(t, int32(31687), existing.Spec.Ports[0].NodePort, "allocated NodePort carried over")
	assert.Equal(t, "nlb", existing.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"], "desired annotation applied")
	assert.Equal(t, "1", existing.Annotations["foreign.io/keep"], "foreign annotation preserved")
}

// TestApplyDesiredServiceFields_NoChangeNoChurn ensures an unchanged service
// reports no change (so reconcileMutableResource skips the Update and doesn't
// churn ResourceVersion every reconcile).
func TestApplyDesiredServiceFields_NoChangeNoChurn(t *testing.T) {
	svc := func() *corev1.Service {
		s := &corev1.Service{
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: map[string]string{"app": "neo4j"},
				Ports:    []corev1.ServicePort{{Name: "bolt", Port: 7687}},
			},
		}
		s.Annotations = map[string]string{"a": "b"}
		return s
	}
	existing := svc()
	existing.Spec.ClusterIP = "10.0.0.5" // server-assigned, absent from desired
	desired := svc()

	// First apply stamps owned-key bookkeeping (a one-time change).
	assert.True(t, applyDesiredServiceFields(existing, desired),
		"first apply stamps ownership bookkeeping")
	// Steady state: an identical desired spec must not churn ResourceVersion.
	assert.False(t, applyDesiredServiceFields(existing, desired),
		"identical desired spec on a stamped service must not report a change")
	assert.Equal(t, "10.0.0.5", existing.Spec.ClusterIP, "ClusterIP still preserved")
}
