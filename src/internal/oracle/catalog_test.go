package oracle

import "testing"

// The catalog is what the documentation and the e2e asserts read, so its shape is a contract of
// its own: no pairing declared twice, and the metadata consistent with where the reason surfaces.
func TestCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Entries() {
		key := e.Condition.String() + "/" + e.Reason.String()
		if seen[key] {
			t.Errorf("%s is catalogued twice", key)
		}
		seen[key] = true

		if e.Reason.String() == "" {
			t.Errorf("%s: empty reason", key)
		}
		if e.Summary == "" {
			t.Errorf("%s: no summary — the generated table would have an empty cell", key)
		}
		switch e.Severity {
		case SeverityError, SeverityWarn, SeverityInfo:
		default:
			t.Errorf("%s: severity %q is not one of error/warn/info", key, e.Severity)
		}
		if e.Nominal && e.Severity != SeverityInfo {
			t.Errorf("%s: a reason that means things are fine cannot be %s", key, e.Severity)
		}
		if e.Condition.IsZero() && e.Surface != SurfaceEvent {
			t.Errorf("%s: no condition carries it, so its surface must be %s, not %s", key, SurfaceEvent, e.Surface)
		}
		if !e.Condition.IsZero() && e.Surface == SurfaceEvent {
			t.Errorf("%s: surface %s contradicts the condition it is declared on", key, SurfaceEvent)
		}
	}
}

// A condition with no reason would be written to a CR with an empty reason field, which the API
// server rejects.
func TestEveryConditionHasReasons(t *testing.T) {
	for _, c := range Conditions() {
		if len(ReasonsFor(c)) == 0 {
			t.Errorf("condition %s declares no reason", c)
		}
	}
}

func TestLookupFindsBothPlacementsOfASharedReason(t *testing.T) {
	for _, c := range []Condition{ConditionClusterFormed, ConditionServersPendingDrain} {
		if _, ok := Lookup(c, ReasonUnsupportedSinglePrimary); !ok {
			t.Errorf("%s/%s missing — scale-in reports it on both", c, ReasonUnsupportedSinglePrimary)
		}
	}
}
