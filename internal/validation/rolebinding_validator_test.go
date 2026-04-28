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

package validation

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func sampleBinding(username string, roles []string) *neo4jv1beta1.Neo4jRoleBinding {
	return &neo4jv1beta1.Neo4jRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jRoleBindingSpec{
			ClusterRef: "c",
			Username:   username,
			Roles:      roles,
		},
	}
}

func TestRoleBindingValidator_Happy(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"reader"}))
	if len(res.Errors) > 0 {
		t.Fatalf("expected no errors, got %v", res.Errors)
	}
}

func TestRoleBindingValidator_RolesRequired(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", nil))
	if len(res.Errors) == 0 {
		t.Fatalf("expected error for empty roles, got none")
	}
}

func TestRoleBindingValidator_EmptyRoleEntryRejected(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"reader", ""}))
	if len(res.Errors) == 0 {
		t.Fatalf("expected error for empty role entry, got none")
	}
}

func TestRoleBindingValidator_PublicWarning(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"PUBLIC", "reader"}))
	if len(res.Warnings) == 0 {
		t.Fatalf("expected a warning for explicit PUBLIC, got none")
	}
}

func TestRoleBindingValidator_ClusterRefMissing(t *testing.T) {
	cl := newUserValidatorClient(t)
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"reader"}))
	if len(res.Errors) == 0 {
		t.Fatalf("expected error for missing clusterRef, got none")
	}
}

func TestRoleBindingValidator_OverlapsWithNeo4jUser(t *testing.T) {
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice-user", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:        "c",
			Username:          "alice",
			PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "x"},
		},
	}
	cl := newUserValidatorClient(t, sampleCluster(), user)
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"reader"}))
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e.Error(), "Neo4jUser") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an error about overlapping Neo4jUser, got %v", res.Errors)
	}
}

func TestRoleBindingValidator_NoOverlapDifferentUsername(t *testing.T) {
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "bob-user", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:        "c",
			Username:          "bob",
			PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "x"},
		},
	}
	cl := newUserValidatorClient(t, sampleCluster(), user)
	v := NewRoleBindingValidator(cl)
	res := v.Validate(context.Background(), sampleBinding("alice", []string{"reader"}))
	if len(res.Errors) > 0 {
		t.Fatalf("expected no overlap errors when usernames differ, got %v", res.Errors)
	}
}
