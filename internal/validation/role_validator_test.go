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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func newRoleValidatorClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestRoleValidator_NameRules(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewRoleValidator(newRoleValidatorClient(t, cluster))

	cases := []struct {
		name      string
		roleName  string
		wantError bool
	}{
		{"valid", "analytics_reader", false},
		{"starts with digit", "9foo", true},
		{"hyphen rejected", "my-role", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			role := &neo4jv1beta1.Neo4jRole{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
				Spec:       neo4jv1beta1.Neo4jRoleSpec{ClusterRef: "c", Name: tc.roleName},
			}
			res := v.Validate(context.Background(), role)
			gotError := len(res.Errors) > 0
			if gotError != tc.wantError {
				t.Fatalf("name %q: gotError=%v wantError=%v errs=%v", tc.roleName, gotError, tc.wantError, res.Errors)
			}
		})
	}
}

func TestRoleValidator_FallbackToMetadataName(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewRoleValidator(newRoleValidatorClient(t, cluster))
	role := &neo4jv1beta1.Neo4jRole{
		ObjectMeta: metav1.ObjectMeta{Name: "analytics_reader", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jRoleSpec{ClusterRef: "c"},
	}
	res := v.Validate(context.Background(), role)
	if len(res.Errors) > 0 {
		t.Fatalf("expected metadata.name fallback to validate cleanly, got %v", res.Errors)
	}
}

func TestRoleValidator_BuiltinGuard(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewRoleValidator(newRoleValidatorClient(t, cluster))

	role := &neo4jv1beta1.Neo4jRole{
		ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jRoleSpec{ClusterRef: "c", Name: "reader"},
	}
	res := v.Validate(context.Background(), role)
	if len(res.Errors) == 0 {
		t.Fatalf("expected built-in role rejection, got no errors")
	}

	role.Spec.AdoptBuiltin = true
	res = v.Validate(context.Background(), role)
	if len(res.Errors) > 0 {
		t.Fatalf("expected built-in adoption to succeed, got %v", res.Errors)
	}
}

func TestRoleValidator_PrivilegeShape(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewRoleValidator(newRoleValidatorClient(t, cluster))

	role := &neo4jv1beta1.Neo4jRole{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jRoleSpec{
			ClusterRef: "c",
			Name:       "r",
			Privileges: []string{
				"GRANT ACCESS ON DATABASE x TO r",
				"DENY WRITE ON GRAPH * TO r",
				"GRANT ACCESS ON DATABASE x TO somebodyelse", // wrong role
				"REVOKE ACCESS ON DATABASE x FROM r",         // wrong verb
				"",                                           // empty
			},
		},
	}
	res := v.Validate(context.Background(), role)
	if len(res.Errors) < 3 {
		t.Fatalf("expected at least 3 errors (wrong-role, wrong-verb, empty), got %d: %v", len(res.Errors), res.Errors)
	}
}

func TestRoleValidator_ClusterRefMissing(t *testing.T) {
	v := NewRoleValidator(newRoleValidatorClient(t))
	role := &neo4jv1beta1.Neo4jRole{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jRoleSpec{ClusterRef: "missing", Name: "r"},
	}
	res := v.Validate(context.Background(), role)
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e.Error(), "missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an error about the missing clusterRef, got %v", res.Errors)
	}
}

func TestIsBuiltInRole(t *testing.T) {
	for _, name := range []string{"PUBLIC", "reader", "editor", "publisher", "architect", "admin"} {
		if !IsBuiltInRole(name) {
			t.Errorf("IsBuiltInRole(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"analytics_reader", "Reader", "Admin"} {
		if IsBuiltInRole(name) {
			t.Errorf("IsBuiltInRole(%q) = true, want false", name)
		}
	}
}
