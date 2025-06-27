# Neo4jPlugin

This document provides a reference for the `Neo4jPlugin` Custom Resource Definition (CRD).

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterName` | `string` | The name of the cluster to install the plugin in. |
| `pluginName` | `string` | The name of the plugin. |
| `version` | `string` | The version of the plugin. |
| `source` | `PluginSource` | The source of the plugin. |
