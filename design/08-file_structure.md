Target structure

```
neo4j-operator/
├── api/v1beta1/
│   ├── common_types.go              # PersistenceSpec, TrustSpec, TopologySpec…
│   ├── neo4j_types.go               # Neo4j / Neo4jList  ← ONLY infra CRD
│   ├── neo4jdatabase_types.go       # spec.neo4jRef (formerly clusterRef)
│   ├── neo4jbackup_types.go
│   ├── neo4jrestore_types.go
│   ├── neo4jplugin_types.go
│   ├── neo4juser_types.go / role / rolebinding / authrule
│   └── zz_generated.deepcopy.go
│
├── internal/
│   ├── validation/
│   │   ├── neo4j_validator.go       # replaces cluster + standalone validators
│   │   ├── topology_validator.go    # servers==1 vs >=2 branches
│   │   └── …                        # database, backup, restore unchanged (neo4jRef)
│   │
│   ├── render/                      # pure K8s objects (formerly resources/)
│   │   ├── workload/
│   │   │   ├── statefulset.go
│   │   │   └── volume_claim.go
│   │   ├── connectivity/
│   │   │   ├── services.go
│   │   │   ├── discovery_rbac.go    # rendered only if servers>=2
│   │   │   ├── ingress.go
│   │   │   └── route.go
│   │   ├── trust/
│   │   ├── serverconfig/
│   │   ├── backup/
│   │   └── restore/
│   │
│   ├── domain/                      # business logic — branches on Mode(neo4j)
│   │   ├── neo4j/
│   │   │   ├── mode.go              # IsCluster(n), IsStandalone(n), StatefulSetName()
│   │   │   └── target.go            # Neo4jTarget interface
│   │   ├── workload/
│   │   │   ├── reconcile.go         # shared pipeline
│   │   │   ├── apply_statefulset.go
│   │   │   └── env_merge.go
│   │   ├── persistence/
│   │   ├── connectivity/
│   │   │   └── discovery.go         # no-op if servers==1
│   │   ├── trust/
│   │   ├── serverconfig/
│   │   ├── scheduling/              # active if servers>=2
│   │   ├── formation/               # active if servers>=2
│   │   ├── maintenance/             # SM upgrade if servers>=2; simple upgrade if ==1
│   │   ├── backup/
│   │   ├── restore/
│   │   └── monitoring/
│   │
│   ├── status/
│   │   ├── conditions.go
│   │   └── writer.go
│   │
│   ├── neo4j/                       # Bolt client
│   │   ├── client.go
│   │   ├── database.go
│   │   └── cluster.go
│   │
│   ├── controller/                  # 1 reconciler PER CRD
│   │   ├── neo4j/                   # ← UNIQUE infra controller
│   │   │   ├── reconciler.go        # pipeline
│   │   │   ├── rbac.go
│   │   │   └── watches.go
│   │   ├── neo4jdatabase/
│   │   ├── neo4jshardeddatabase/
│   │   ├── neo4jbackup/
│   │   ├── neo4jrestore/
│   │   ├── neo4jplugin/
│   │   └── auth/
│   │       ├── user/
│   │       ├── role/
│   │       ├── rolebinding/
│   │       └── authrule/
│   │
│   ├── migration/                   # NO webhook — offline tools
│   │   ├── cluster_to_neo4j.go      # converts cluster YAML manifest → Neo4j
│   │   ├── standalone_to_neo4j.go
│   │   └── ref_rewriter.go          # clusterRef → neo4jRef in dependent CRs
│   │
│   └── monitoring/
│
├── cmd/
│   ├── manager/main.go              # operator
│   └── migrate/main.go              # migration CLI (optional)
│
├── config/crd/bases/
│   └── neo4j.neo4j.com_neo4js.yaml  # new
│   # deprecated: neo4jenterpriseclusters, neo4jenterprisestandalones (storage:false)
│
├── test/
│   ├── integration/
│   │   ├── neo4j/                   # formerly cluster + standalone
│   │   │   ├── single_node_test.go  # servers:1
│   │   │   └── ha_cluster_test.go   # servers:3
│   │   └── domain/
│   │       ├── persistence/
│   │       ├── maintenance/
│   │       ├── formation/
│   │       └── backup_restore/
│   └── unit/
│
└── design/
    ├── 001-layering.md              # render / domain / controller
    ├── 002-unified-neo4j-crd.md     # decision + spec
    ├── 003-migration-guide.md       # cluster/standalone → Neo4j
    └── package-map.md
```
