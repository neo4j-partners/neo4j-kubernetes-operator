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

// Sharded database events
const (
	EventReasonShardedDatabaseReady = "ShardedDatabaseReady"
	EventReasonClusterNotReady      = "ClusterNotReady"
	EventReasonClientCreationFailed = "ClientCreationFailed"
)
