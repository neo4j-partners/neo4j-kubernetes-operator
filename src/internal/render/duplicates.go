package render

import (
	"fmt"
	"sort"
)

// Origins of a value taking part in a duplicate — reported as-is on the Event so a user can
// tell their own input from what the operator or Neo4j contributed.
const (
	OriginUser             = "user"
	OriginNeo4jDefault     = "neo4j-default"
	OriginOperatorDefault  = "operator-default"
	OriginOperatorInjected = "operator-injected"
	OriginPluginDefinition = "plugin-definition"
)

// Duplicate is one value dropped while merging a CR field with defaults, plugin configuration
// or operator-injected settings. Merges are deterministic but silent in the rendered output,
// so the reconciler reports every Duplicate (operator log + Event, reason DuplicateEntry).
// Any field that merges layers can produce these — it is not tied to one part of the spec.
type Duplicate struct {
	// Field is the CR path the collision belongs to, e.g. spec.config.jvm.additionalArguments.
	Field string
	// Key is the identity used for dedupe inside that field (a conf key, a flag name).
	Key string
	// Kept is the value present in the rendered output, KeptFrom the layer it came from.
	Kept     string
	KeptFrom string
	// Dropped is the value it replaced, DroppedFrom the layer that lost.
	Dropped     string
	DroppedFrom string
}

// Message is the one-line rendering shared by the Event and the operator log.
func (d Duplicate) Message() string {
	return fmt.Sprintf("%s: duplicate entry for %s — kept %q (%s), dropped %q (%s)",
		d.Field, d.Key, d.Kept, d.KeptFrom, d.Dropped, d.DroppedFrom)
}

// SortDuplicates orders entries so repeated reconciles report them identically — layers built
// from Go maps would otherwise emit them in random order.
func SortDuplicates(dups []Duplicate) []Duplicate {
	sort.SliceStable(dups, func(i, j int) bool {
		if dups[i].Field != dups[j].Field {
			return dups[i].Field < dups[j].Field
		}
		return dups[i].Key < dups[j].Key
	})
	return dups
}
