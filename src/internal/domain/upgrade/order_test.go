package upgrade

import "testing"

func TestHoldPrimaries(t *testing.T) {
	lagging := PoolState{Desired: 2, Updated: 0, Ready: 2, OnTarget: false} // not given the new image yet
	moving := PoolState{Desired: 2, Updated: 1, Ready: 1, Rolling: true, OnTarget: true}
	done := converged(2)

	cases := []struct {
		name        string
		running     string
		secondaries []PoolState
		want        bool
	}{
		{"first install never holds", "", []PoolState{lagging}, false},
		{"no version change never holds", "2026.07.0", []PoolState{lagging}, false},
		{"secondary not yet on the new image", "2026.05.0", []PoolState{lagging}, true},
		{"secondary still rolling", "2026.05.0", []PoolState{moving}, true},
		{"secondary converged", "2026.05.0", []PoolState{done}, false},
		{
			// The window this feature is most likely to get wrong: the operator has just written the
			// new image to the pool's template, so OnTarget is already true, but the StatefulSet
			// controller has not yet acted and its status still describes the pods it has — all of
			// them updated, ready, and on one revision. Every field but Settled says "finished".
			// Releasing the primaries here upgrades the system primary before the secondary has
			// restarted at all, which is the one order Neo4j does not allow.
			"secondary whose status has not caught up with its new template",
			"2026.05.0",
			[]PoolState{{Desired: 2, Updated: 2, Ready: 2, OnTarget: true, Settled: false}},
			true,
		},
		{"one of two secondaries lagging", "2026.05.0", []PoolState{done, lagging}, true},
		{"both secondaries converged", "2026.05.0", []PoolState{done, done}, false},
		{"cluster with no secondary pools has nothing to wait for", "2026.05.0", nil, false},
		{
			"secondary on the new image but a member is not back",
			"2026.05.0",
			[]PoolState{{Desired: 2, Updated: 2, Ready: 1, OnTarget: true}},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := cr(tc.running, "2026.07.0")
			if got := HoldPrimaries(n, tc.secondaries); got != tc.want {
				t.Errorf("HoldPrimaries = %v, want %v", got, tc.want)
			}
		})
	}
}

// A configuration change must roll every pool exactly as it did before this existed: the versions
// agree, so there is no upgrade to order.
func TestHoldPrimariesIgnoresConfigChanges(t *testing.T) {
	n := cr("2026.07.0", "2026.07.0")
	midRoll := []PoolState{{Desired: 2, Updated: 0, Ready: 1, Rolling: true, OnTarget: true}}
	if HoldPrimaries(n, midRoll) {
		t.Error("a config-change roll must not be held: only a version change is ordered")
	}
}
