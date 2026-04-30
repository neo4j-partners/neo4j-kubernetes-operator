package controller

// Cluster formation events
const (
	EventReasonClusterFormationStarted = "ClusterFormationStarted"
	EventReasonClusterFormationFailed  = "ClusterFormationFailed"
	EventReasonClusterReady            = "ClusterReady"
	EventReasonTopologyWarning         = "TopologyWarning"
	EventReasonValidationFailed        = "ValidationFailed"
	EventReasonTopologyPlacementFailed = "TopologyPlacementFailed"
	EventReasonTopologyPlacementCalc   = "TopologyPlacementCalculated"
	EventReasonPropertyShardingFailed  = "PropertyShardingValidationFailed"
	EventReasonServerRoleFailed        = "ServerRoleValidationFailed"
	EventReasonRouteAPINotFound        = "RouteAPINotFound"
	EventReasonMCPApocMissing          = "MCPApocMissing"
	EventReasonReconcileFailed         = "ReconcileFailed"
)

// Rolling upgrade events
const (
	EventReasonUpgradeStarted    = "UpgradeStarted"
	EventReasonUpgradeCompleted  = "UpgradeCompleted"
	EventReasonUpgradePaused     = "UpgradePaused"
	EventReasonUpgradeFailed     = "UpgradeFailed"
	EventReasonUpgradeRolledBack = "UpgradeRolledBack"
)

// Backup and restore events
const (
	EventReasonBackupScheduled      = "BackupScheduled"
	EventReasonBackupStarted        = "BackupStarted"
	EventReasonBackupCompleted      = "BackupCompleted"
	EventReasonBackupFailed         = "BackupFailed"
	EventReasonRestoreStarted       = "RestoreStarted"
	EventReasonRestoreCompleted     = "RestoreCompleted"
	EventReasonRestoreFailed        = "RestoreFailed"
	EventReasonDatabaseCreateFailed = "DatabaseCreateFailed"
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
	EventReasonPluginEnabled       = "PluginEnabled"
	EventReasonPluginDisabled      = "PluginDisabled"
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
