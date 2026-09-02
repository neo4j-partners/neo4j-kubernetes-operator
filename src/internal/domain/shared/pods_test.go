package shared

import "testing"

func TestParseNamedArtifacts(t *testing.T) {
	got := ParseNamedArtifacts(
		"neo4j=neo4j-2026-09-01T15-08-49.backup|4096\n" + // name + size
			"movies=movies-2026.backup\n" + // name only (old Job / stat failed)
			"empty=|10\n" + // no name -> skipped
			"broken line without equals\n" +
			"foo=foo.backup|notanumber\n" + // bad size -> size 0, name kept
			"=orphan\n") // no key -> skipped

	if len(got) != 3 {
		t.Fatalf("parsed %d entries, want 3: %+v", len(got), got)
	}
	if a := got["neo4j"]; a.Name != "neo4j-2026-09-01T15-08-49.backup" || a.SizeBytes != 4096 {
		t.Errorf("neo4j = %+v, want name+size 4096", a)
	}
	if a := got["movies"]; a.Name != "movies-2026.backup" || a.SizeBytes != 0 {
		t.Errorf("movies = %+v, want name only, size 0", a)
	}
	if a := got["foo"]; a.Name != "foo.backup" || a.SizeBytes != 0 {
		t.Errorf("foo = %+v, want name kept, size 0 on unparseable bytes", a)
	}
	if _, ok := got["empty"]; ok {
		t.Error("empty name must be skipped")
	}
}
