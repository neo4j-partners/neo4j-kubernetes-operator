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
		"Every claim the operator manages is Bound and serving the size the spec asks for")
	ConditionTLSReady = declareCondition("TLSReady", GateFalseBlocks,
		"Trust is disabled, or every required TLS Secret and key is present")
	ConditionClusterFormed = declareCondition("ClusterFormed", GateClusterFalseBlocks,
		"Every desired server is enabled in the Neo4j cluster")
	ConditionServersPendingDrain = declareCondition("ServersPendingDrain", GateClusterTrueBlocks,
		"A server dropped from the spec is still registered in Neo4j and waiting to be drained")
	ConditionBackupReady = declareCondition("BackupReady", GateFalseBlocks,
		"At least one successful backup exists for this Neo4j instance")
	ConditionRestoreReady = declareCondition("RestoreReady", GateFalseBlocks,
		"At least one successful restore exists for this Neo4j instance")
	ConditionScheduleReady = declareCondition("ScheduleReady", GateNone,
		"The backup schedule's cron cadences are valid and active, or intentionally suspended")
)

// Ready — the headline condition (ADR-004).
var (
	ReasonAllMembersReady = declareNominal("AllMembersReady", SurfaceCondition,
		on(ConditionReady, "Every desired server is ready and the CR is serving"))
	ReasonMembersNotReady = declare("MembersNotReady", SeverityWarn, SurfaceCondition,
		on(ConditionReady, "Fewer servers ready than desired; the message carries both counts"))
	ReasonTLSNotReady = declare("TLSNotReady", SeverityWarn, SurfaceCondition,
		on(ConditionReady, "Held back by TLSReady — trust material is missing or still being issued"))
	ReasonStorageNotReady = declare("StorageNotReady", SeverityWarn, SurfaceCondition,
		on(ConditionReady, "Held back by StorageReady — a claim is unbound, still growing, or smaller than the spec asks for. The members themselves may all be up, which is why this is not MembersNotReady"))
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
	ReasonStorageTemplateDrift = declare("StorageTemplateDrift", SeverityError, SurfaceBoth,
		on(ConditionError, "The volumeClaimTemplates the spec renders differ from the live StatefulSet's in more than size, and Kubernetes accepts no new set. The operator applies nothing rather than leave the pod template mounting a volume no template backs; the message names the volumes that diverge"))
)

// StorageReady — the data claim (BDR-005).
var (
	ReasonPVCBound = declareNominal("PVCBound", SurfaceCondition,
		on(ConditionStorageReady, "The data PVC is Bound"))
	ReasonPVCPending = declare("PVCPending", SeverityWarn, SurfaceCondition,
		on(ConditionStorageReady, "Data PVC not Bound yet; the message names the StorageClass, or reports that none is set"))
	ReasonStorageResizing = declare("StorageResizing", SeverityInfo, SurfaceCondition,
		on(ConditionStorageReady, "A volume grow is in flight: the claims already carry the larger request and the message names those whose capacity has not caught up. Neo4j keeps serving from the old size throughout"))
	ReasonStorageResizeFailed = declare("StorageResizeFailed", SeverityError, SurfaceBoth,
		on(ConditionStorageReady, "A claim is still smaller than the spec asks for. The Event carries the API server's own words, most often a StorageClass with `allowVolumeExpansion: false`; nothing was changed and the old size still serves"))
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

// BackupReady — a Neo4jBackup run-to-completion record (BDR-014 / ADR-015).
var (
	ReasonBackupSucceeded = declareNominal("BackupSucceeded", SurfaceCondition,
		on(ConditionBackupReady, "The backup Job completed and artifacts were written"))
	ReasonBackupInProgress = declare("BackupInProgress", SeverityInfo, SurfaceCondition,
		on(ConditionBackupReady, "The backup Job is running"))
	ReasonBackupJobFailed = declare("BackupJobFailed", SeverityError, SurfaceBoth,
		on(ConditionBackupReady, "The backup Job failed; the message carries the failure detail"))
	ReasonBackupTargetNotFound = declare("BackupTargetNotFound", SeverityWarn, SurfaceCondition,
		on(ConditionBackupReady, "`spec.neo4jRef` does not resolve to a Neo4j in this namespace yet"))
	ReasonBackupEditionUnsupported = declare("BackupEditionUnsupported", SeverityError, SurfaceBoth,
		on(ConditionBackupReady, "Backup requires Enterprise edition; the target is community"))
	ReasonBackupListenerDisabled = declare("BackupListenerDisabled", SeverityWarn, SurfaceCondition,
		on(ConditionBackupReady, "The target has no backup listener; set `features.backup` and `connectivity.listeners.backup`"))
	ReasonBackupDestinationUnsupported = declare("BackupDestinationUnsupported", SeverityError, SurfaceBoth,
		on(ConditionBackupReady, "The `destination` cannot be realized (e.g. PVC provisioning is not yet supported; use an existing claimName)"))
	ReasonBackupSourceNotFound = declare("BackupSourceNotFound", SeverityWarn, SurfaceCondition,
		on(ConditionBackupReady, "`spec.source.backupRef` (type Aggregate) does not resolve to a Succeeded Neo4jBackup yet"))
	ReasonBackupSourceUnsupported = declare("BackupSourceUnsupported", SeverityError, SurfaceBoth,
		on(ConditionBackupReady, "The aggregate source cannot be used (not PVC-backed, missing recorded artifact, or mixed claims)"))
)

// RestoreReady — a Neo4jRestore run-to-completion record (BDR-014 / ADR-015). Restore runs
// over Bolt (seed-from-URI), so its failure modes are Bolt/formation/database ones, not Job ones.
var (
	ReasonRestoreSucceeded = declareNominal("RestoreSucceeded", SurfaceCondition,
		on(ConditionRestoreReady, "Every requested database was seeded and is online"))
	ReasonRestoreInProgress = declare("RestoreInProgress", SeverityInfo, SurfaceCondition,
		on(ConditionRestoreReady, "Databases are being seeded from the source; waiting for them to come online"))
	ReasonRestoreTargetNotFound = declare("RestoreTargetNotFound", SeverityWarn, SurfaceCondition,
		on(ConditionRestoreReady, "`spec.neo4jRef` does not resolve to a Neo4j in this namespace yet"))
	ReasonRestoreEditionUnsupported = declare("RestoreEditionUnsupported", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "Restore requires Enterprise edition; the target is community"))
	ReasonRestoreBeforeFormation = declare("RestoreBeforeFormation", SeverityWarn, SurfaceCondition,
		on(ConditionRestoreReady, "The target is not formation-stable (ClusterFormed) yet; restore waits to avoid seeding over an incomplete server set"))
	ReasonRestoreSourceNotFound = declare("RestoreSourceNotFound", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "`source.backupRef` does not resolve to a succeeded Neo4jBackup, or the resolved artifact has no usable location"))
	ReasonRestoreSourceUnsupported = declare("RestoreSourceUnsupported", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "The source cannot be turned into a seedURI the servers can read (e.g. a PVC-backed artifact requires the RWX `backups` volume path — not yet wired)"))
	ReasonRestoreDatabaseExists = declare("RestoreDatabaseExists", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "A target database already exists and `overwrite` is false; nothing was dropped or seeded"))
	ReasonRestoreBoltUnavailable = declare("RestoreBoltUnavailable", SeverityWarn, SurfaceCondition,
		on(ConditionRestoreReady, "The operator could not reach the target's system database over Bolt; it will retry"))
	ReasonRestoreSeedFailed = declare("RestoreSeedFailed", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "A CREATE/seed statement failed; the message carries the Neo4j error detail"))
	ReasonRestoreAggregating = declare("RestoreAggregating", SeverityInfo, SurfaceCondition,
		on(ConditionRestoreReady, "A pre-seed `neo4j-admin backup aggregate` Job is collapsing the backup chain before seeding"))
	ReasonRestoreAggregateFailed = declare("RestoreAggregateFailed", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "The pre-seed aggregate Job failed; the message carries the neo4j-admin failure detail"))
	ReasonRestoreMetadataApplying = declare("RestoreMetadataApplying", SeverityInfo, SurfaceCondition,
		on(ConditionRestoreReady, "Databases are online; a post-seed Job is reapplying the backed-up users, roles, and privileges to the system database (spec.restoreMetadata)"))
	ReasonRestoreMetadataConflict = declare("RestoreMetadataConflict", SeverityWarn, SurfaceEvent,
		asEvent("Post-seed metadata apply completed with skipped statements (a role/user already existed on the target); the restore still Succeeded and the Event carries the detail"))
	ReasonRestoreMetadataFailed = declare("RestoreMetadataFailed", SeverityError, SurfaceBoth,
		on(ConditionRestoreReady, "The post-seed metadata Job could not run (bad artifact, or the system database was unreachable); the message carries the failure detail"))
)

// Neo4jBackupSchedule — cron owner that emits Neo4jBackup objects (BDR-014 §10).
var (
	ReasonScheduleActive = declareNominal("ScheduleActive", SurfaceCondition,
		on(ConditionScheduleReady, "Cadences are parsed and the schedule is emitting Neo4jBackup objects on time"))
	ReasonScheduleSuspended = declare("ScheduleSuspended", SeverityInfo, SurfaceCondition,
		on(ConditionScheduleReady, "`spec.suspend` is true; no backups are emitted until it is cleared"))
	ReasonScheduleTargetNotFound = declare("ScheduleTargetNotFound", SeverityWarn, SurfaceCondition,
		on(ConditionScheduleReady, "`spec.neo4jRef` does not resolve to a Neo4j in this namespace yet"))
	ReasonScheduleEditionUnsupported = declare("ScheduleEditionUnsupported", SeverityError, SurfaceBoth,
		on(ConditionScheduleReady, "Backup requires Enterprise edition; the target is community"))
	ReasonScheduleInvalidCron = declare("ScheduleInvalidCron", SeverityError, SurfaceBoth,
		on(ConditionScheduleReady, "A cron expression (full or incremental) could not be parsed"))
	ReasonScheduleBackupEmitted = declareNominal("ScheduleBackupEmitted", SurfaceEvent,
		asEvent("A cadence tick emitted a Neo4jBackup; the Event names the backup, its type, and the chain"))
	ReasonSchedulePruned = declareNominal("SchedulePruned", SurfaceEvent,
		asEvent("Retention removed a whole expired backup chain; the Event names the chain and how many backups and artifacts were pruned (BDR-014 §10)"))
	ReasonSchedulePruneFailed = declare("SchedulePruneFailed", SeverityWarn, SurfaceEvent,
		asEvent("The Job that deletes an expired chain's PVC artifacts failed; the chain is kept and retried, and the message carries the failure detail"))
	ReasonSchedulePruneUnsupported = declare("SchedulePruneUnsupported", SeverityWarn, SurfaceEvent,
		asEvent("A chain is eligible for retention pruning but its destination is object storage, which the operator cannot prune yet (pending ADR-016 cloud identity); the chain is kept"))
	ReasonScheduleCompacted = declareNominal("ScheduleCompacted", SurfaceEvent,
		asEvent("Aggregate compaction collapsed a closed chain into its recovered full and pruned the original links (kept the recovered full); the Event names the chain and how many links were pruned (BDR-014 §10)"))
	ReasonScheduleAggregateFailed = declare("ScheduleAggregateFailed", SeverityWarn, SurfaceEvent,
		asEvent("The aggregate backup for a closed chain failed, so its links are kept (not compacted) and retried; the message carries the failure detail"))
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
	ReasonStorageResizeCompleted = declareNominal("StorageResizeCompleted", SurfaceEvent,
		asEvent("Every claim reached the size the spec asks for; the Event names the volume and the new capacity. Emitted on the pass that observes the last claim catch up, not on every pass"))
)
