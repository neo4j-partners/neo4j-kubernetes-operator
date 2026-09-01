/*
Copyright Neo4j.

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

// Package oracle is the single catalog of the condition types, condition reasons and Event
// reasons the operator can emit (ADR-014). Reason and Condition are opaque value types, so a
// reason that is not declared here cannot reach a setCondition or an EventRecorder call: the
// package refuses to compile rather than shipping an identifier no test and no page knows about.
//
// All three projections are generated from this file by `make errors` and must not be
// hand-edited: the tables in docs/user-guide/05-reference/errors.md, the condition table in
// docs/design/crd-spec/neo4j/status.md, and the shell oracle in tests/lib/oracle.sh, which the
// e2e asserts source instead of copying reason strings.
package oracle

// Condition is a status.conditions[].type the operator writes. The zero value marks a reason
// carried by an Event alone.
type Condition struct {
	name string
	gate Gate
	// summary states what True means, and is the generated status contract — so it describes
	// what the writer measures, not what a future version might measure.
	summary string
}

func (c Condition) String() string { return c.name }

// IsZero reports whether the condition is the Event-only placeholder.
func (c Condition) IsZero() bool { return c.name == "" }

// Summary states what True means for this condition.
func (c Condition) Summary() string { return c.summary }

// Gate reports how this condition holds Ready back.
func (c Condition) Gate() Gate { return c.gate }

// Gate is a condition's effect on Ready, as internal/status.Writer computes it. Kept next to the
// condition because the published contract is what gates Ready, and a doc paragraph drifts from
// the writer while a declared value is regenerated with it.
type Gate uint8

const (
	// GateSelf marks Ready itself.
	GateSelf Gate = iota
	// GateNone marks a condition Ready is computed independently of.
	GateNone
	// GateFalseBlocks marks a condition Ready needs True.
	GateFalseBlocks
	// GateTrueBlocks marks a condition Ready needs False.
	GateTrueBlocks
	// GateClusterFalseBlocks needs True, in Cluster mode only.
	GateClusterFalseBlocks
	// GateClusterTrueBlocks needs False, in Cluster mode only.
	GateClusterTrueBlocks
)

// String is the cell the generated contract table shows.
func (g Gate) String() string {
	switch g {
	case GateSelf:
		return "— this *is* `Ready`"
	case GateFalseBlocks:
		return "Yes — `False` holds `Ready` back"
	case GateTrueBlocks:
		return "Yes — `True` clears `Ready`"
	case GateClusterFalseBlocks:
		return "Cluster mode — `False` holds `Ready` back"
	case GateClusterTrueBlocks:
		return "Cluster mode — `True` holds `Ready` back"
	default:
		return "No"
	}
}

// Reason is a stable status.conditions[].reason — and the Event reason where the same
// identifier carries both surfaces. It is a contract: runbooks, alerts and e2e asserts match
// on it, whereas the human-readable message may be reworded at any time.
type Reason struct{ name string }

func (r Reason) String() string { return r.name }

// Severity tells an operator what a reason deserves: error means the operator stopped making
// progress and needs a human, warn means it is waiting on something that may resolve itself,
// info narrates an operation in progress or a settled state.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
)

// Surface is where a reason shows up. Reasons on the Error condition are also recorded as a
// Warning Event under the same identifier, so one catalog row covers both.
type Surface string

const (
	SurfaceCondition Surface = "condition"
	SurfaceEvent     Surface = "event"
	SurfaceBoth      Surface = "condition+event"
)

// Entry is one catalogued outcome: a reason as it appears under one condition, or as an
// Event-only reason when Condition is zero.
type Entry struct {
	Condition Condition
	Reason    Reason
	Severity  Severity
	Surface   Surface
	// Nominal marks a reason that can only mean things are fine; the generated documentation
	// lists those apart so the error reference stays a reference of problems.
	Nominal bool
	Summary string
}

var (
	conditions []Condition
	entries    []Entry
)

// Conditions returns every condition type the operator writes, in declaration order.
func Conditions() []Condition { return append([]Condition(nil), conditions...) }

// Entries returns the whole catalog in declaration order — the source of the generated
// documentation tables and of tests/lib/oracle.sh.
func Entries() []Entry { return append([]Entry(nil), entries...) }

// ReasonsFor returns the reasons the operator can put on one condition, in declaration order.
func ReasonsFor(c Condition) []Reason {
	var out []Reason
	for _, e := range entries {
		if e.Condition == c {
			out = append(out, e.Reason)
		}
	}
	return out
}

// Lookup returns the catalog row for a (condition, reason) pair.
func Lookup(c Condition, r Reason) (Entry, bool) {
	for _, e := range entries {
		if e.Condition == c && e.Reason == r {
			return e, true
		}
	}
	return Entry{}, false
}

func declareCondition(name string, gate Gate, summary string) Condition {
	c := Condition{name: name, gate: gate, summary: summary}
	conditions = append(conditions, c)
	return c
}

// placement is one condition a reason appears under, with the summary that fits that pairing —
// UnsupportedSinglePrimary means something different on ClusterFormed and on ServersPendingDrain.
type placement struct {
	condition Condition
	summary   string
}

func on(c Condition, summary string) placement { return placement{condition: c, summary: summary} }

// asEvent places a reason on no condition: the CR is untouched, the operator only wants the
// user to know it resolved something.
func asEvent(summary string) placement { return placement{summary: summary} }

// declare registers a reason that reports a problem, and returns the only value able to carry
// it to an emission site.
func declare(name string, severity Severity, surface Surface, places ...placement) Reason {
	return register(name, severity, surface, false, places)
}

// declareNominal registers a reason that can only mean things are fine (severity info).
func declareNominal(name string, surface Surface, places ...placement) Reason {
	return register(name, SeverityInfo, surface, true, places)
}

func register(name string, severity Severity, surface Surface, nominal bool, places []placement) Reason {
	r := Reason{name: name}
	for _, p := range places {
		entries = append(entries, Entry{
			Condition: p.condition,
			Reason:    r,
			Severity:  severity,
			Surface:   surface,
			Nominal:   nominal,
			Summary:   p.summary,
		})
	}
	return r
}

// Conditions the operator writes, with what True means and how each one gates Ready — the
// summaries and gates below are the published contract, so they track internal/status.Writer and
// nothing else. Declared before any reason so both registries keep file order, which is the order
// the generated tables read in.
var (
	ConditionReady = declareCondition("Ready", GateSelf,
		"Every desired server is ready, the data claims are bound, trust material is in place, and in Cluster mode the cluster is formed with no drain outstanding")
	ConditionReconciling = declareCondition("Reconciling", GateNone,
		"A reconcile pass is in flight; the writer clears it at the end of every pass, so it narrates progress rather than gating anything")
	ConditionInstalled = declareCondition("Installed", GateFalseBlocks,
		"At least one StatefulSet exists for the active pools")
	ConditionError = declareCondition("Error", GateTrueBlocks,
		"The last pipeline pass returned an error; the same reason is recorded as a Warning Event")
	ConditionStorageReady = declareCondition("StorageReady", GateFalseBlocks,
		"Every data PVC the operator manages is Bound")
	ConditionTLSReady = declareCondition("TLSReady", GateFalseBlocks,
		"Trust is disabled, or every required TLS Secret and key is present")
	ConditionClusterFormed = declareCondition("ClusterFormed", GateClusterFalseBlocks,
		"Every desired server is enabled in the Neo4j cluster")
	ConditionServersPendingDrain = declareCondition("ServersPendingDrain", GateClusterTrueBlocks,
		"A server dropped from the spec is still registered in Neo4j and waiting to be drained")
)

// Ready — the headline condition (ADR-004).
var (
	ReasonAllMembersReady = declareNominal("AllMembersReady", SurfaceCondition,
		on(ConditionReady, "Every desired server is ready and the CR is serving"))
	ReasonMembersNotReady = declare("MembersNotReady", SeverityWarn, SurfaceCondition,
		on(ConditionReady, "Fewer servers ready than desired; the message carries both counts"))
	ReasonTLSNotReady = declare("TLSNotReady", SeverityWarn, SurfaceCondition,
		on(ConditionReady, "Held back by TLSReady — trust material is missing or still being issued"))
	ReasonOfflineMaintenance = declare("OfflineMaintenance", SeverityInfo, SurfaceCondition,
		on(ConditionReady, "`spec.maintenance.offlineMode` is true, so the Neo4j process is not running"))
	ReasonReconcileError = declare("ReconcileError", SeverityError, SurfaceCondition,
		on(ConditionReady, "Ready cleared because reconcile failed"))
)

// Reconciling — one pass of the pipeline (ADR-003).
var (
	ReasonInProgress = declareNominal("InProgress", SurfaceCondition,
		on(ConditionReconciling, "A reconcile pass is running"))
	ReasonCompleted = declareNominal("Completed", SurfaceCondition,
		on(ConditionReconciling, "The last reconcile pass finished"))
	ReasonFailed = declare("Failed", SeverityError, SurfaceCondition,
		on(ConditionReconciling, "Reconciling stopped after failure"))
)

// Installed — the operands exist.
var (
	ReasonObjectsCreated = declareNominal("ObjectsCreated", SurfaceCondition,
		on(ConditionInstalled, "The operands exist — at least one StatefulSet was observed"))
	ReasonPending = declare("Pending", SeverityInfo, SurfaceCondition,
		on(ConditionInstalled, "No StatefulSet observed yet; expected on a first pass, a symptom if it lasts"))
)

// Error — carried by the condition and by a Warning Event under the same reason.
var (
	ReasonNoError = declareNominal("NoError", SurfaceCondition,
		on(ConditionError, "No pipeline error on the last pass"))
	ReasonReconcileFailed = declare("ReconcileFailed", SeverityError, SurfaceBoth,
		on(ConditionError, "A pipeline step returned an error"))
	ReasonSecretNotMountable = declare("SecretNotMountable", SeverityError, SurfaceBoth,
		on(ConditionError, "Referenced Secret lacks the `neo4j.com/mountable-by-operator` opt-in label (NEO-005)"))
	ReasonSecretNotDelegated = declare("SecretNotDelegated", SeverityError, SurfaceBoth,
		on(ConditionError, "BYO auth Secret is not delegated to this Neo4j via `neo4j.com/allowed-for` (ADD-01)"))
	ReasonAuthSecretInvalid = declare("AuthSecretInvalid", SeverityError, SurfaceBoth,
		on(ConditionError, "Auth Secret holds a `NEO4J_AUTH` value the Neo4j image entrypoint cannot use; the pod would crash-loop"))
)

// StorageReady — the data claim (BDR-005).
var (
	ReasonPVCBound = declareNominal("PVCBound", SurfaceCondition,
		on(ConditionStorageReady, "The data PVC is Bound"))
	ReasonPVCPending = declare("PVCPending", SeverityWarn, SurfaceCondition,
		on(ConditionStorageReady, "Data PVC not Bound yet; the message names the StorageClass, or reports that none is set"))
)

// TLSReady — trust material (BDR-006).
var (
	ReasonTrustDisabled = declareNominal("TrustDisabled", SurfaceCondition,
		on(ConditionTLSReady, "`trust.enabled` is false, so there is nothing to verify"))
	ReasonSecretsPresent = declareNominal("SecretsPresent", SurfaceCondition,
		on(ConditionTLSReady, "Required TLS secrets and keys are present"))
	ReasonSecretMissing = declare("SecretMissing", SeverityError, SurfaceCondition,
		on(ConditionTLSReady, "Required TLS/auth Secret is missing or incomplete"))
	ReasonCertificatePending = declare("CertificatePending", SeverityWarn, SurfaceCondition,
		on(ConditionTLSReady, "Waiting for cert-manager to issue the certificate into the operator-provisioned Secret"))
)

// ClusterFormed and ServersPendingDrain — cluster formation and scale-in (ADR-007).
var (
	ReasonFormed = declareNominal("Formed", SurfaceCondition,
		on(ConditionClusterFormed, "All desired servers are enabled in the Neo4j cluster"))
	ReasonEnablingServer = declare("EnablingServer", SeverityInfo, SurfaceCondition,
		on(ConditionClusterFormed, "`ENABLE SERVER` in progress for a server that joined the pool"))
	ReasonBoltUnavailable = declare("BoltUnavailable", SeverityWarn, SurfaceCondition,
		on(ConditionClusterFormed, "Cannot reach Bolt to form or align the cluster"))
	ReasonBootstrapGateTooHigh = declare("BootstrapGateTooHigh", SeverityError, SurfaceCondition,
		on(ConditionClusterFormed, "`topology.minimumMembers` asks for more primaries than the pool has, so the system database never bootstraps and Bolt never answers"))
	ReasonShowServersFailed = declare("ShowServersFailed", SeverityError, SurfaceCondition,
		on(ConditionClusterFormed, "`SHOW SERVERS` failed over Bolt"))
	ReasonUnsupportedSystemScaleUp = declare("UnsupportedSystemScaleUp", SeverityError, SurfaceCondition,
		on(ConditionClusterFormed, "Cannot grow the system database from a single primary via `ENABLE SERVER` alone"))
	ReasonWaitingSystemLeader = declare("WaitingSystemLeader", SeverityWarn, SurfaceCondition,
		on(ConditionClusterFormed, "Waiting for a system database leader"))
	ReasonWaitingQuorum = declare("WaitingQuorum", SeverityWarn, SurfaceCondition,
		on(ConditionClusterFormed, "Waiting for primary quorum, or for an enable to complete"))
	ReasonUnsupportedSinglePrimary = declare("UnsupportedSinglePrimary", SeverityError, SurfaceCondition,
		on(ConditionClusterFormed, "Neo4j forbids shrinking to a single primary"),
		on(ConditionServersPendingDrain, "Drain blocked — it would leave a single primary"))
	ReasonNoDrain = declareNominal("NoDrain", SurfaceCondition,
		on(ConditionServersPendingDrain, "No server is waiting to be drained"))
	ReasonShrinkingTopology = declare("ShrinkingTopology", SeverityInfo, SurfaceCondition,
		on(ConditionServersPendingDrain, "Scale-in in progress"))
	ReasonDraining = declare("Draining", SeverityInfo, SurfaceCondition,
		on(ConditionServersPendingDrain, "Server drain / `DEALLOCATE DATABASES` in progress"))
	ReasonAwaitingSTSShrink = declare("AwaitingSTSShrink", SeverityInfo, SurfaceCondition,
		on(ConditionServersPendingDrain, "Waiting for the StatefulSet replica shrink after drain"))
	ReasonDrainTimeout = declare("DrainTimeout", SeverityWarn, SurfaceBoth,
		on(ConditionServersPendingDrain, "A scale-in has stayed pending past the operator's budget; the message names the member Neo4j has not released, what it still reports it hosting, and how long the scale-in has waited. The StatefulSet stays at its current size — no data is at risk, but the scale-in needs a look"))
)

// Event-only reasons: the CR stays healthy, the operator reports a decision or restates the
// spec. Advisories among them are recorded once per generation (internal/events.Advisory).
var (
	ReasonDuplicateEntry = declare("DuplicateEntry", SeverityWarn, SurfaceEvent,
		asEvent("Two values collided on the same key in a spec field; the Event names the field, the value kept and the one dropped"))
	ReasonDatabaseTopologyResized = declare("DatabaseTopologyResized", SeverityWarn, SurfaceEvent,
		asEvent("A scale-in forced `ALTER DATABASE SET TOPOLOGY` on a database wider than the remaining pool; the Event names the database and both counts, before and after"))
	ReasonInsecureAdminConnection = declare("InsecureAdminConnection", SeverityWarn, SurfaceEvent,
		asEvent("The operator's own admin Bolt connection is unencrypted because `trust.insecureAdminConnection` is true (NEO-004)"))
	ReasonAdminBoltTLSRequired = declare("AdminBoltTLSRequired", SeverityWarn, SurfaceEvent,
		asEvent("The operator refuses to dial admin Bolt without `trust.certificates.bolt` or `trust.insecureAdminConnection` (NEO-004)"))
	ReasonSecretMounted = declareNominal("SecretMounted", SurfaceEvent,
		asEvent("A labelled Secret is being mounted into the Neo4j pods; the Event names the Secret and the opt-in label"))
)
