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
	"strings"
	"testing"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// TestBuildShardedDatabaseOptions_Parameterised pins issue #170: every
// user-supplied seed value is bound as a driver parameter (no raw
// interpolation), seedConfig is the documented comma-separated string, and
// seedRestoreUntil uses the integer/datetime() forms — never a `SEED CONFIG`
// clause or quoted literals.
func TestBuildShardedDatabaseOptions_Parameterised(t *testing.T) {
	sdb := &neo4jv1beta1.Neo4jShardedDatabase{
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			SeedURI:            "s3://b/x' OR '1'='1 //",
			SeedSourceDatabase: "src' //",
			SeedConfig: &neo4jv1beta1.SeedConfiguration{
				RestoreUntil: "txId:99",
				Config:       map[string]string{"region": "eu-west-1"},
			},
			TxLogEnrichment: "FULL",
		},
	}
	clause, params, err := buildShardedDatabaseOptions(sdb)
	if err != nil {
		t.Fatal(err)
	}
	// No raw value may appear in the clause text.
	for _, leak := range []string{"s3://", "1'='1", "src'", "eu-west-1"} {
		if strings.Contains(clause, leak) {
			t.Fatalf("value leaked into clause %q (found %q)", clause, leak)
		}
	}
	for _, want := range []string{
		"seedURI: $seed_uri",
		"seedSourceDatabase: $seed_source_database",
		"seedConfig: $seed_config",
		"seedRestoreUntil: $seed_restore_until",
		"txLogEnrichment: $tx_log_enrichment",
	} {
		if !strings.Contains(clause, want) {
			t.Fatalf("expected %q in clause %q", want, clause)
		}
	}
	if params["seed_uri"] != "s3://b/x' OR '1'='1 //" {
		t.Errorf("seed_uri param = %v", params["seed_uri"])
	}
	if params["seed_config"] != "region=eu-west-1" {
		t.Errorf("seed_config param = %v", params["seed_config"])
	}
	if params["seed_restore_until"] != int64(99) {
		t.Errorf("seed_restore_until param = %T %v", params["seed_restore_until"], params["seed_restore_until"])
	}
}

func TestBuildShardedDatabaseOptions_PerShardSeedURIs(t *testing.T) {
	sdb := &neo4jv1beta1.Neo4jShardedDatabase{
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			SeedURIs: map[string]string{"g000": "s3://b/g.backup", "p000": "s3://b/p.backup"},
		},
	}
	clause, params, err := buildShardedDatabaseOptions(sdb)
	if err != nil {
		t.Fatal(err)
	}
	// Keys are interpolated (backticked); values are parameters.
	if !strings.Contains(clause, "`g000`: $seed_uri_0") || !strings.Contains(clause, "`p000`: $seed_uri_1") {
		t.Fatalf("expected per-shard backticked keys with param values, got %q", clause)
	}
	if strings.Contains(clause, "s3://") {
		t.Fatalf("per-shard URI leaked into clause %q", clause)
	}
	if params["seed_uri_0"] != "s3://b/g.backup" || params["seed_uri_1"] != "s3://b/p.backup" {
		t.Fatalf("per-shard URI params wrong: %v", params)
	}
}

func TestBuildShardedDatabaseOptions_SeedURIMutex(t *testing.T) {
	sdb := &neo4jv1beta1.Neo4jShardedDatabase{
		Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
			SeedURI:  "s3://b/x",
			SeedURIs: map[string]string{"g000": "s3://b/g"},
		},
	}
	if _, _, err := buildShardedDatabaseOptions(sdb); err == nil {
		t.Fatal("expected error when both seedURI and seedURIs are set")
	}
}
