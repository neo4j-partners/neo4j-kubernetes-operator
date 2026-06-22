/*
Copyright 2025 Priyo Lahiri.

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
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// Pins the #263 connectivity-failure forensics: the streak counts
// consecutive failures per cluster, preserves the first-failure timestamp,
// and resets on success so the next outage starts a fresh streak.
func TestConnectivityFailureStreak(t *testing.T) {
	r := &Neo4jEnterpriseClusterReconciler{}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns1"},
	}
	other := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns2"},
	}

	s1 := r.recordConnectivityFailure(cluster)
	if s1.count != 1 {
		t.Fatalf("first failure: count = %d, want 1", s1.count)
	}
	firstSince := s1.since

	s2 := r.recordConnectivityFailure(cluster)
	if s2.count != 2 {
		t.Fatalf("second failure: count = %d, want 2", s2.count)
	}
	if !s2.since.Equal(firstSince) {
		t.Fatalf("streak start moved on repeat failure: %v != %v", s2.since, firstSince)
	}

	// Streaks are per cluster — a different namespace starts at 1.
	if got := r.recordConnectivityFailure(other); got.count != 1 {
		t.Fatalf("other cluster streak: count = %d, want 1", got.count)
	}

	// Success resets the streak; the next failure starts over.
	r.clearConnectivityFailures(context.Background(), cluster)
	if got := r.recordConnectivityFailure(cluster); got.count != 1 {
		t.Fatalf("post-reset failure: count = %d, want 1", got.count)
	}

	// Clearing a cluster with no streak is a no-op.
	r.clearConnectivityFailures(context.Background(), other)
	r.clearConnectivityFailures(context.Background(), other)
}

func TestFirstNonNilErr(t *testing.T) {
	e1, e2 := errors.New("one"), errors.New("two")
	if got := firstNonNilErr(nil, e1, e2); !errors.Is(got, e1) {
		t.Fatalf("firstNonNilErr = %v, want %v", got, e1)
	}
	if got := firstNonNilErr(nil, nil); got != nil {
		t.Fatalf("firstNonNilErr all-nil = %v, want nil", got)
	}
}
