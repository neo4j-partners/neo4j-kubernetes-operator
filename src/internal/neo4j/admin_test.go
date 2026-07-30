package neo4j

import "testing"

func TestParseAuthSecret(t *testing.T) {
	u, p, err := ParseAuthSecret("neo4j/s3cret")
	if err != nil || u != "neo4j" || p != "s3cret" {
		t.Fatalf("got %q %q %v", u, p, err)
	}
	if _, _, err := ParseAuthSecret("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFindActiveByAddressSkipsDropped(t *testing.T) {
	servers := []Server{
		{Name: "old", Address: "host:7687", State: "Dropped"},
		{Name: "new", Address: "host:7687", State: "Free"},
	}
	s, ok := FindActiveByAddress(servers, "host")
	if !ok || s.Name != "new" {
		t.Fatalf("got %#v ok=%v", s, ok)
	}
	onlyDropped := []Server{{Name: "old", Address: "host:7687", State: "Dropped"}}
	if _, ok := FindActiveByAddress(onlyDropped, "host"); ok {
		t.Fatal("dropped-only should miss")
	}
}
