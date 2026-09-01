package neo4j

import (
	"crypto/x509"
	"strings"
	"testing"
)

func TestParseAuthSecret(t *testing.T) {
	u, p, err := ParseAuthSecret("neo4j/s3cret")
	if err != nil || u != "neo4j" || p != "s3cret" {
		t.Fatalf("got %q %q %v", u, p, err)
	}
	if _, _, err := ParseAuthSecret("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRewriteAdminURI(t *testing.T) {
	pool := x509.NewCertPool()
	got, err := rewriteAdminURI("neo4j://db.svc:7687", ConnectOpts{RootCAs: pool})
	if err != nil || got != "neo4j+s://db.svc:7687" {
		t.Fatalf("tls got %q %v", got, err)
	}
	got, err = rewriteAdminURI("neo4j+ssc://db.svc:7687", ConnectOpts{RootCAs: pool})
	if err != nil || got != "neo4j+s://db.svc:7687" {
		t.Fatalf("upgrade ssc got %q %v", got, err)
	}
	got, err = rewriteAdminURI("neo4j://db.svc:7687", ConnectOpts{AllowPlaintext: true})
	if err != nil || got != "neo4j://db.svc:7687" {
		t.Fatalf("plain got %q %v", got, err)
	}
	got, err = rewriteAdminURI("neo4j+ssc://db.svc:7687", ConnectOpts{AllowPlaintext: true})
	if err != nil || got != "neo4j://db.svc:7687" {
		t.Fatalf("strip ssc got %q %v", got, err)
	}
	if _, err := rewriteAdminURI("neo4j://db.svc:7687", ConnectOpts{}); err == nil || !strings.Contains(err.Error(), "insecureAdminConnection") {
		t.Fatalf("expected fail-closed, got %v", err)
	}
	if _, err := rewriteAdminURI("neo4j://db.svc:7687", ConnectOpts{RootCAs: pool, AllowPlaintext: true}); err == nil {
		t.Fatal("expected mutual exclusion error")
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

// HostsOnly decides whether a departing member still owes anyone data, so a wrong answer either
// wedges a scale-in or drops a member that was still hosting a database.
func TestHostsOnly(t *testing.T) {
	exempt := map[string]bool{"system": true, "shards": true}
	cases := []struct {
		name    string
		hosting []string
		want    bool
	}{
		{"drained down to system", []string{"system"}, true},
		{"still hosting a user database", []string{"neo4j", "system"}, false},
		{"composites do not count — Neo4j lists them on every server", []string{"system", "shards"}, true},
		{"a database not in the exempt set counts, composite-looking name or not", []string{"system", "orders"}, false},
		// Neo4j reports its own spelling; the catalog and the CR need not agree on case.
		{"case and padding are Neo4j's, not ours", []string{" System ", "SHARDS"}, true},
		{"nothing reported at all", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HostsOnly(Server{Name: "r1", Hosting: tc.hosting}, exempt); got != tc.want {
				t.Errorf("HostsOnly(%v) = %v, want %v", tc.hosting, got, tc.want)
			}
		})
	}
}
