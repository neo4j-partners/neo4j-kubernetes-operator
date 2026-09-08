package upgrade

import (
	"errors"
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// cr builds a minimal Neo4j whose only interesting fields are the running and desired versions.
func cr(running, desired string) *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{}
	n.Spec.Version = desired
	n.Status.Version = running
	return n
}

func TestPreflight(t *testing.T) {
	digest := func(n *neo4jv1beta1.Neo4j) *neo4jv1beta1.Neo4j {
		n.Spec.Image = &neo4jv1beta1.ImageSpec{Digest: "sha256:" + strings.Repeat("a", 64)}
		return n
	}
	offline := func(n *neo4jv1beta1.Neo4j) *neo4jv1beta1.Neo4j {
		n.Spec.Maintenance = &neo4jv1beta1.MaintenanceSpec{OfflineMode: true}
		return n
	}
	plugins := func(mode neo4jv1beta1.VolumeMode) func(*neo4jv1beta1.Neo4j) *neo4jv1beta1.Neo4j {
		return func(n *neo4jv1beta1.Neo4j) *neo4jv1beta1.Neo4j {
			n.Spec.Storage = &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Plugins: &neo4jv1beta1.AuxiliaryVolumeSpec{Mode: mode},
				},
			}
			return n
		}
	}

	cases := []struct {
		name    string
		neo4j   *neo4jv1beta1.Neo4j
		wantErr error // nil means accepted
	}{
		// Accepted.
		{"first install has no running version", cr("", "2026.05.0"), nil},
		{"no change", cr("2026.05.0", "2026.05.0"), nil},
		{"calver patch bump", cr("2026.05.0", "2026.05.1"), nil},
		{"calver one hop across releases", cr("2025.01.0", "2026.07.1"), nil},
		{"5.26 LTS into calver", cr("5.26.0", "2026.05.0"), nil},
		{"5.26 without patch component into calver", cr("5.26", "2025.01.0"), nil},
		{"minor bump inside 5.x", cr("5.20.0", "5.26.0"), nil},
		{"enterprise suffix is not part of the comparison", cr("2026.05.0-enterprise", "2026.07.0-enterprise"), nil},
		{"unparseable versions are not blocked", cr("latest", "also-not-a-version"), nil},

		// R4 — downgrade is refused outright, whatever the size of the step.
		{"calver downgrade", cr("2026.07.0", "2026.05.0"), ErrVersionDowngrade},
		{"patch downgrade", cr("2026.05.1", "2026.05.0"), ErrVersionDowngrade},
		{"calver back to 5.x", cr("2025.01.0", "5.26.0"), ErrVersionDowngrade},

		// R5 — LTS checkpoints cannot be skipped.
		{"pre-5.26 cannot jump to calver", cr("5.20.0", "2025.01.0"), ErrVersionUpgrade},
		{"5.25 is still short of the checkpoint", cr("5.25.0", "2026.05.0"), ErrVersionUpgrade},

		// Refusals a user can act on.
		{"digest pin makes the bump a no-op", digest(cr("2026.05.0", "2026.07.0")), ErrVersionUpgrade},
		{"offline mode", offline(cr("2026.05.0", "2026.07.0")), ErrVersionUpgrade},
		{"existing plugins volume cannot be refreshed", plugins(neo4jv1beta1.VolumeModeExisting)(cr("2026.05.0", "2026.07.0")), ErrVersionUpgrade},
		{"share plugins volume is fine", plugins(neo4jv1beta1.VolumeModeShare)(cr("2026.05.0", "2026.07.0")), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Preflight(tc.neo4j)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("expected the change to be accepted, got: %v", err)
			case tc.wantErr != nil && err == nil:
				t.Fatalf("expected %v, got no error", tc.wantErr)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("expected %v, got: %v", tc.wantErr, err)
			}
		})
	}
}

// A refusal reaches the user as a message, so it has to name the versions rather than only the rule.
func TestPreflightMessageNamesBothVersions(t *testing.T) {
	err := Preflight(cr("2026.07.0", "2026.05.0"))
	if err == nil {
		t.Fatal("expected a downgrade refusal")
	}
	for _, want := range []string{"2026.05.0", "2026.07.0", "restoring a backup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal message does not mention %q: %s", want, err)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"5.26.0", "5.26", 0},
		{"5.26", "5.26.0", 0},
		{"5.9.0", "5.10.0", -1}, // numeric, not lexical
		{"2026.05.0", "2026.5.0", 0},
		{"2025.01.0", "2026.05.0", -1},
		{"5.26.0", "2025.01.0", -1},
		{"2026.05.0.1", "2026.05.0", 1}, // optional fourth component
	}
	for _, tc := range cases {
		a, ok := parseVersion(tc.a)
		if !ok {
			t.Fatalf("could not parse %q", tc.a)
		}
		b, ok := parseVersion(tc.b)
		if !ok {
			t.Fatalf("could not parse %q", tc.b)
		}
		if got := a.compare(b); got != tc.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseVersionRejectsNonNumeric(t *testing.T) {
	for _, s := range []string{"", "latest", "-enterprise", "v"} {
		if _, ok := parseVersion(s); ok {
			t.Errorf("parseVersion(%q) reported success; unparseable tags must be recognised as such", s)
		}
	}
}
