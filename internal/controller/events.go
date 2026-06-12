package controller

// Cluster formation events
const (
	EventReasonClusterFormationStarted       = "ClusterFormationStarted"
	EventReasonClusterFormationFailed        = "ClusterFormationFailed"
	EventReasonClusterReady                  = "ClusterReady"
	EventReasonTopologyWarning               = "TopologyWarning"
	EventReasonValidationFailed              = "ValidationFailed"
	EventReasonTopologyPlacementFailed       = "TopologyPlacementFailed"
	EventReasonTopologyPlacementCalc         = "TopologyPlacementCalculated"
	EventReasonTopologyZoneDiscoveryDegraded = "TopologyZoneDiscoveryDegraded"
	EventReasonPropertyShardingFailed        = "PropertyShardingValidationFailed"
	EventReasonServerRoleFailed              = "ServerRoleValidationFailed"
	EventReasonRouteAPINotFound              = "RouteAPINotFound"
	EventReasonMCPApocMissing                = "MCPApocMissing"
	EventReasonReconcileFailed               = "ReconcileFailed"
	EventReasonScaleDownPendingDrain         = "ScaleDownPendingDrain"
	EventReasonScaleDownDraining             = "ScaleDownDraining"
	EventReasonScaleDownBlocked              = "ScaleDownBlocked"
	// EventReasonConnectivityDegraded — the operator has failed to reach the
	// cluster's Bolt endpoint for a sustained streak of reconciles (#263).
	EventReasonConnectivityDegraded = "ConnectivityDegraded"
)

// Rolling upgrade events
const (
	EventReasonUpgradeStarted    = "UpgradeStarted"
	EventReasonUpgradeCompleted  = "UpgradeCompleted"
	EventReasonUpgradePaused     = "UpgradePaused"
	EventReasonUpgradeFailed     = "UpgradeFailed"
	EventReasonUpgradeRolledBack = "UpgradeRolledBack"
	// EventReasonUpgradeDeferred — an image upgrade was requested while a
	// scale-down drain is in progress; the upgrade starts once the drain
	// completes (#173/#174 mutual exclusion).
	EventReasonUpgradeDeferred = "UpgradeDeferred"
)

// Backup and restore events
const (
	EventReasonBackupScheduled = "BackupScheduled"
	// EventReasonBackupRetentionCaveat — retention pruning configured on a CR
	// whose chain may contain differential artifacts (#217).
	EventReasonBackupRetentionCaveat = "BackupRetentionCaveat"
	// EventReasonServiceAccountAnnotationConflict — a backup/restore CR
	// overwrote DIFFERENT workload-identity annotations on the shared
	// namespace ServiceAccount; last writer wins and the others' cloud
	// access breaks (#227).
	EventReasonServiceAccountAnnotationConflict = "ServiceAccountAnnotationConflict"
	// EventReasonSeedEndpointNotProjected — a cluster Cypher restore
	// resolved to a custom S3 endpoint (MinIO etc.) but the server pods
	// verifiably lack AWS_ENDPOINT_URL_S3, so the server-side seed fetch
	// will target s3.amazonaws.com and fail (#252).
	EventReasonSeedEndpointNotProjected = "SeedEndpointNotProjected"
	EventReasonBackupStarted            = "BackupStarted"
	EventReasonBackupCompleted          = "BackupCompleted"
	EventReasonBackupFailed             = "BackupFailed"
	EventReasonRestoreStarted           = "RestoreStarted"
	EventReasonRestoreCompleted         = "RestoreCompleted"
	EventReasonRestoreFailed            = "RestoreFailed"
	EventReasonRestoreFromChainParent   = "RestoreFromChainParent"
	EventReasonDatabaseCreateFailed     = "DatabaseCreateFailed"
)

// Database events
const (
	EventReasonClusterNotFound     = "ClusterNotFound"
	EventReasonDatabaseReady       = "DatabaseReady"
	EventReasonDatabaseDeleted     = "DatabaseDeleted"
	EventReasonDatabaseCreatedSeed = "DatabaseCreatedFromSeed"
	EventReasonCreationFailed      = "CreationFailed"
	EventReasonDeletionFailed      = "DeletionFailed"
	EventReasonDataImported        = "DataImported"
	EventReasonDataImportFailed    = "DataImportFailed"
	EventReasonDataSeeded          = "DataSeeded"
	EventReasonValidationWarning   = "ValidationWarning"
	EventReasonConnectionFailed    = "ConnectionFailed"
)

// Plugin events
const (
	EventReasonPluginInstalled     = "PluginInstalled"
	EventReasonPluginInstallFailed = "PluginInstallFailed"
	// EventReasonPluginDuplicate is emitted on a Neo4jPlugin when another
	// CR in the same namespace targets the same clusterRef with the same
	// spec.name. The reconciler refuses to install when this would cause
	// two reconcilers to race on the same /plugins directory + the same
	// NEO4J_PLUGINS env value; the older CR (by creationTimestamp) wins
	// and the newer one is marked Failed.
	EventReasonPluginDuplicate = "PluginDuplicate"
)

// Split-brain events
const (
	EventReasonSplitBrainDetected     = "SplitBrainDetected"
	EventReasonSplitBrainRepaired     = "SplitBrainRepaired"
	EventReasonSplitBrainRepairFailed = "SplitBrainRepairFailed"
)

// Aura Fleet Management events
const (
	EventReasonAuraFleetFailed            = "AuraFleetManagementFailed"
	EventReasonAuraFleetPluginPatchFailed = "AuraFleetManagementPluginPatchFailed"
	EventReasonAuraFleetRegistered        = "AuraFleetManagementRegistered"
)

// Storage expansion events
const (
	EventReasonStorageExpansionStarted   = "StorageExpansionStarted"
	EventReasonStorageExpansionCompleted = "StorageExpansionCompleted"
	EventReasonStorageExpansionFailed    = "StorageExpansionFailed"
)

// Storage configuration events
const (
	EventReasonStorageClassNotFound = "StorageClassNotFound"
)

// Sharded database events
const (
	EventReasonShardedDatabaseReady = "ShardedDatabaseReady"
	EventReasonClusterNotReady      = "ClusterNotReady"
	EventReasonClientCreationFailed = "ClientCreationFailed"
)

// User and role management events
const (
	EventReasonUserCreated         = "UserCreated"
	EventReasonUserUpdated         = "UserUpdated"
	EventReasonUserDeleted         = "UserDeleted"
	EventReasonUserReady           = "UserReady"
	EventReasonUserDeletionFailed  = "UserDeletionFailed"
	EventReasonUserSyncFailed      = "UserSyncFailed"
	EventReasonPasswordRotated     = "PasswordRotated"
	EventReasonRolesGranted        = "RolesGranted"
	EventReasonRolesRevoked        = "RolesRevoked"
	EventReasonRolePending         = "RolePending"
	EventReasonRoleCreated         = "RoleCreated"
	EventReasonRoleDeleted         = "RoleDeleted"
	EventReasonRoleReady           = "RoleReady"
	EventReasonRoleDeletionFailed  = "RoleDeletionFailed"
	EventReasonRoleSyncFailed      = "RoleSyncFailed"
	EventReasonPrivilegesApplied   = "PrivilegesApplied"
	EventReasonPrivilegesDriftKept = "PrivilegesDriftKept"
)

// User and role-binding management events
const (
	EventReasonBindingCreated = "BindingCreated"
	EventReasonBindingUpdated = "BindingUpdated"
	EventReasonBindingDeleted = "BindingDeleted"
	EventReasonBindingFailed  = "BindingFailed"
	EventReasonUserNotFound   = "UserNotFound"
)

// Attribute-based access control (Neo4jAuthRule) events.
const (
	EventReasonAuthRuleCreated           = "AuthRuleCreated"
	EventReasonAuthRuleUpdated           = "AuthRuleUpdated"
	EventReasonAuthRuleDeleted           = "AuthRuleDeleted"
	EventReasonAuthRuleFailed            = "AuthRuleFailed"
	EventReasonAuthRuleVersionTooOld     = "AuthRuleVersionTooOld"
	EventReasonOIDCProviderNotConfigured = "OIDCProviderNotConfigured"
)

// Conditions used by the user/role/binding reconcilers.
const (
	ConditionTypeRolesSynced         = "RolesSynced"
	ConditionTypePasswordSynced      = "PasswordSynced"
	ConditionTypePendingDependencies = "PendingDependencies"
	ConditionTypeClusterNotReady     = "ClusterNotReady"
	ConditionTypePrivilegesSynced    = "PrivilegesSynced"
	ConditionTypeUserNotFound        = "UserNotFound"
)

const (
	ConditionReasonRolesSynced       = "RolesMatch"
	ConditionReasonRolesPending      = "RolesPending"
	ConditionReasonPasswordSynced    = "PasswordMatchesSecret"
	ConditionReasonClusterNotReady   = "ClusterNotReady"
	ConditionReasonUserReady         = "UserReady"
	ConditionReasonRoleReady         = "RoleReady"
	ConditionReasonPrivilegesSynced  = "PrivilegesMatch"
	ConditionReasonPrivilegesDrifted = "PrivilegesDrifted"
)
