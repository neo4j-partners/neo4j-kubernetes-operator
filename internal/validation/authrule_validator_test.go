/*
Copyright 2026.

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

func newAuthRuleValidatorClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestAuthRuleValidator_NameRules(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewAuthRuleValidator(newAuthRuleValidatorClient(t, cluster))

	cases := []struct {
		name    string
		ruleNm  string
		wantErr bool
	}{
		{"valid simple", "salesRule", false},
		{"valid with hyphen", "sales-rule-v2", false},
		{"valid with underscore", "sales_rule", false},
		{"starts with digit", "1rule", true},
		{"empty falls back to metadata.name (also valid)", "", false},
		{"contains space", "sales rule", true},
		{"contains dot", "sales.rule", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &neo4jv1beta1.Neo4jAuthRule{
				ObjectMeta: metav1.ObjectMeta{Name: "metadata-name", Namespace: "ns"},
				Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
					ClusterRef:   "c",
					Name:         tc.ruleNm,
					Condition:    "abac.oidc.user_attribute('dept') = 'sales'",
					GrantedRoles: []string{"reader"},
				},
			}
			res := v.Validate(context.Background(), r)
			gotErr := false
			for _, e := range res.Errors {
				if strings.Contains(e.Field, "spec.name") {
					gotErr = true
				}
			}
			if gotErr != tc.wantErr {
				t.Errorf("name=%q: gotErr=%v, want %v; errors=%v", tc.ruleNm, gotErr, tc.wantErr, res.Errors)
			}
		})
	}
}

func TestAuthRuleValidator_ConditionGuards(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewAuthRuleValidator(newAuthRuleValidatorClient(t, cluster))

	// Condition contains a forbidden DDL keyword. Each entry should produce
	// at least one error pointing at spec.condition.
	bad := []string{
		"CREATE ROLE attacker",
		"abac.oidc.user_attribute('x') = 'y' ; DROP ROLE admin",
		"abac.oidc.user_attribute('x') = 'y' AND GRANT reader TO admin",
		"SHOW USERS",
		"alter user neo4j set password 'pwn'",
		"", // empty
	}
	for _, c := range bad {
		t.Run(c, func(t *testing.T) {
			r := &neo4jv1beta1.Neo4jAuthRule{
				ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "ns"},
				Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
					ClusterRef:   "c",
					Name:         "rule",
					Condition:    c,
					GrantedRoles: []string{"reader"},
				},
			}
			res := v.Validate(context.Background(), r)
			found := false
			for _, e := range res.Errors {
				if strings.Contains(e.Field, "spec.condition") {
					found = true
				}
			}
			if !found {
				t.Errorf("condition %q: expected a spec.condition error, got: %v", c, res.Errors)
			}
		})
	}

	// Conditions that look DDL-adjacent but aren't (e.g. the substring
	// "create" embedded in a string literal) must NOT trip the guard. The
	// boundary check requires the keyword to be a standalone token.
	good := []string{
		"abac.oidc.user_attribute('createdAt') > date()",
		"abac.oidc.user_attribute('department') = 'engineering'",
		"abac.oidc.user_attribute('region') = 'EMEA' AND time.transaction('UTC').hour < 18",
	}
	for _, c := range good {
		t.Run("good:"+c, func(t *testing.T) {
			r := &neo4jv1beta1.Neo4jAuthRule{
				ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "ns"},
				Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
					ClusterRef:   "c",
					Name:         "rule",
					Condition:    c,
					GrantedRoles: []string{"reader"},
				},
			}
			res := v.Validate(context.Background(), r)
			for _, e := range res.Errors {
				if strings.Contains(e.Field, "spec.condition") {
					t.Errorf("condition %q: did not expect a spec.condition error, got: %v", c, e)
				}
			}
		})
	}
}

func TestAuthRuleValidator_ClusterRefMissing(t *testing.T) {
	v := NewAuthRuleValidator(newAuthRuleValidatorClient(t))
	rule := &neo4jv1beta1.Neo4jAuthRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
			ClusterRef:   "missing",
			Name:         "rule",
			Condition:    "abac.oidc.user_attribute('dept') = 'sales'",
			GrantedRoles: []string{"reader"},
		},
	}
	res := v.Validate(context.Background(), rule)
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

func TestAuthRuleValidator_GrantedRolesEmpty(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
	v := NewAuthRuleValidator(newAuthRuleValidatorClient(t, cluster))
	rule := &neo4jv1beta1.Neo4jAuthRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
			ClusterRef:   "c",
			Name:         "rule",
			Condition:    "abac.oidc.user_attribute('dept') = 'sales'",
			GrantedRoles: nil,
		},
	}
	res := v.Validate(context.Background(), rule)
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e.Field, "spec.grantedRoles") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error on empty grantedRoles, got: %v", res.Errors)
	}
}
