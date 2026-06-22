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

import "testing"

// TestParseAllDatabaseArtifactsFromLog verifies the per-database artifact map
// the all-databases backup records for cluster-wide restore (#222): one entry
// per user database, shard physical databases excluded, last-occurrence wins.
func TestParseAllDatabaseArtifactsFromLog(t *testing.T) {
	log := `
Backup of database 'neo4j' completed, written to /backups/neo4j-2026-06-08T01-18-06.backup
Backup of database 'customers' completed, written to /backups/customers-2026-06-08T01-19-12.backup
Backup of database 'products-g000' completed, written to /backups/products-g000-2026-06-08T01-20-00.backup
Backup of database 'products-p000' completed, written to /backups/products-p000-2026-06-08T01-20-30.backup
re-run: customers written to /backups/customers-2026-06-08T02-00-00.backup
`
	got := parseAllDatabaseArtifactsFromLog(log)

	want := map[string]string{
		"neo4j":     "neo4j-2026-06-08T01-18-06.backup",
		"customers": "customers-2026-06-08T02-00-00.backup", // last occurrence wins
	}
	if len(got) != len(want) {
		t.Fatalf("got %d artifacts %+v, want %d (%v)", len(got), got, len(want), want)
	}
	for _, a := range got {
		w, ok := want[a.Database]
		if !ok {
			t.Errorf("unexpected database %q (shard databases must be excluded)", a.Database)
			continue
		}
		if a.Filename != w {
			t.Errorf("database %q: filename = %q, want %q", a.Database, a.Filename, w)
		}
	}
}

// TestParseAllDatabaseArtifactsFromLog_Empty ensures a garbled/empty log is
// non-fatal and yields no artifacts.
func TestParseAllDatabaseArtifactsFromLog_Empty(t *testing.T) {
	if got := parseAllDatabaseArtifactsFromLog(""); len(got) != 0 {
		t.Fatalf("expected no artifacts for empty log, got %+v", got)
	}
	if got := parseAllDatabaseArtifactsFromLog("no backup files here"); len(got) != 0 {
		t.Fatalf("expected no artifacts for non-matching log, got %+v", got)
	}
}
