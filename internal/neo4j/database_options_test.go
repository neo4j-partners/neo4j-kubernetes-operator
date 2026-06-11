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

package neo4j

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// TestBuildOptionsClause_ParameterisesValues pins the Cypher-injection defense:
// seedURI and every option VALUE must become a driver parameter, never appear
// in the clause text. Keys are interpolated (operator/enum-controlled).
func TestBuildOptionsClause_ParameterisesValues(t *testing.T) {
	c := &Client{}
	evil := "s3://b/x' OR '1'='1 //"
	clause, params := c.buildOptionsClause(map[string]string{"existingData": "use"}, evil, nil)

	if strings.Contains(clause, "s3://") || strings.Contains(clause, "1'='1") {
		t.Fatalf("seedURI value leaked into clause text: %q", clause)
	}
	if !strings.Contains(clause, "seedURI: $opt_seedURI") {
		t.Fatalf("expected parameterised seedURI, got: %q", clause)
	}
	if !strings.Contains(clause, "existingData: $opt_existingData") {
		t.Fatalf("expected parameterised option, got: %q", clause)
	}
	if params["opt_seedURI"] != evil {
		t.Fatalf("seedURI must be carried verbatim as a param, got: %v", params["opt_seedURI"])
	}
	if params["opt_existingData"] != "use" {
		t.Fatalf("option value missing from params: %v", params)
	}
}

// TestBuildOptionsClause_Deterministic ensures the clause order is stable
// (keys sorted) so the same input doesn't churn.
func TestBuildOptionsClause_Deterministic(t *testing.T) {
	c := &Client{}
	opts := map[string]string{"existingData": "use", "storeFormat": "block"}
	first, _ := c.buildOptionsClause(opts, "s3://b/x", nil)
	for i := 0; i < 20; i++ {
		got, _ := c.buildOptionsClause(opts, "s3://b/x", nil)
		if got != first {
			t.Fatalf("clause order not deterministic: %q vs %q", first, got)
		}
	}
}

// TestBuildOptionsClause_Empty returns no clause when there are no options.
func TestBuildOptionsClause_Empty(t *testing.T) {
	c := &Client{}
	if clause, params := c.buildOptionsClause(nil, "", nil); clause != "" || params != nil {
		t.Fatalf("expected empty clause/params, got %q / %v", clause, params)
	}
}

func TestCypherLanguageClause_OnlyValidVersions(t *testing.T) {
	if got := cypherLanguageClause("25"); got != " DEFAULT LANGUAGE CYPHER 25" {
		t.Errorf("25: got %q", got)
	}
	if got := cypherLanguageClause("5"); got != " DEFAULT LANGUAGE CYPHER 5" {
		t.Errorf("5: got %q", got)
	}
	for _, bad := range []string{"", "6", "25; DROP DATABASE neo4j", "5 OPTIONS{}"} {
		if got := cypherLanguageClause(bad); got != "" {
			t.Errorf("expected empty clause for %q, got %q", bad, got)
		}
	}
}

// TestBuildOptionsClause_SeedConfigAndRestoreUntil pins the documented seed
// OPTIONS form (issue #169): seedConfig is a comma-separated key=value STRING
// passed as a parameter, and seedRestoreUntil is an integer txId ($p) or an
// RFC3339 timestamp wrapped in datetime($p) — never a `SEED CONFIG {…}` clause.
func TestBuildOptionsClause_SeedConfigAndRestoreUntil(t *testing.T) {
	c := &Client{}

	t.Run("seedConfig serialised as comma-separated string param", func(t *testing.T) {
		sc := &neo4jv1beta1.SeedConfiguration{Config: map[string]string{"region": "eu-west-1", "endpoint": "x"}}
		clause, params := c.buildOptionsClause(nil, "s3://b/x", sc)
		if !strings.Contains(clause, "seedConfig: $opt_seedConfig") {
			t.Fatalf("expected parameterised seedConfig, got: %q", clause)
		}
		if strings.Contains(clause, "SEED CONFIG") {
			t.Fatalf("must not emit the non-grammar SEED CONFIG clause: %q", clause)
		}
		// sorted, comma-separated key=value
		if params["opt_seedConfig"] != "endpoint=x,region=eu-west-1" {
			t.Fatalf("seedConfig serialisation = %v", params["opt_seedConfig"])
		}
	})

	t.Run("restoreUntil txId is an integer param", func(t *testing.T) {
		sc := &neo4jv1beta1.SeedConfiguration{RestoreUntil: "txId:12345"}
		clause, params := c.buildOptionsClause(nil, "s3://b/x", sc)
		if !strings.Contains(clause, "seedRestoreUntil: $opt_seedRestoreUntil") {
			t.Fatalf("expected integer seedRestoreUntil param, got: %q", clause)
		}
		if params["opt_seedRestoreUntil"] != int64(12345) {
			t.Fatalf("expected int64 12345, got %T %v", params["opt_seedRestoreUntil"], params["opt_seedRestoreUntil"])
		}
	})

	t.Run("restoreUntil RFC3339 wrapped in datetime()", func(t *testing.T) {
		sc := &neo4jv1beta1.SeedConfiguration{RestoreUntil: "2025-01-15T10:30:00Z"}
		clause, params := c.buildOptionsClause(nil, "s3://b/x", sc)
		if !strings.Contains(clause, "seedRestoreUntil: datetime($opt_seedRestoreUntil)") {
			t.Fatalf("expected datetime()-wrapped seedRestoreUntil, got: %q", clause)
		}
		if params["opt_seedRestoreUntil"] != "2025-01-15T10:30:00Z" {
			t.Fatalf("expected RFC3339 string param, got %v", params["opt_seedRestoreUntil"])
		}
	})
}

func TestSerializeSeedConfig(t *testing.T) {
	if got := serializeSeedConfig(nil); got != "" {
		t.Errorf("nil → %q", got)
	}
	if got := serializeSeedConfig(map[string]string{"b": "2", "a": "1"}); got != "a=1,b=2" {
		t.Errorf("sorted serialisation = %q, want a=1,b=2", got)
	}
}
