---
title: Neo4j Kubernetes Operator — Functional Requirements
markmap:
  colorFreezeLevel: 2
---

# Neo4j Kubernetes Operator

## 🗺️ Legend
- 🟢 V1 (in scope)
- 🟣 V2 (out of V1 / deferred)
- ✅ Implemented
- ⚠️ Implemented with known issue
- ⬜ Not implemented
- 🧪 Automated tests

## ⚙️ Operator (OP)

### OP-1-001 Install the Operator 🟢 ✅
- OP-2-001-PKG-01 YAML 🟢 ✅
- OP-2-001-PKG-02 Helm 🟣 ✅
- OP-2-001-SCOPE-01 Single namespace 🟢 ✅
- OP-2-001-SCOPE-02 Multiple namespaces 🟣 ✅
- OP-2-001-SCOPE-03 Cluster-wide 🟣 ⬜

### OP-1-002 Reconcile desired state 🟢 ✅

### OP-1-003 Report status 🟢 ✅
- OP-2-003-STATUS-01 Basic conditions 🟢 ✅
- OP-2-003-STATUS-02 Detailed phase/status 🟣 ⬜

### OP-1-004 Upgrade the Operator 🟣 ⬜
- OP-2-004-UPG-01 In-place controller upgrade 🟣 ⬜
- OP-2-004-UPG-02 CRD conversion webhook 🟣 ⬜

### OP-1-005 Uninstall the Operator 🟢 ✅
- OP-2-005-UNINST-01 Preserve data 🟢 ✅
- OP-2-005-UNINST-02 Delete data 🟣 ✅

### OP-1-006 Manage permissions (RBAC) 🟢 ✅ 🧪

### OP-1-007 Metrics and logs 🟣 ⬜

## 🗄️ Neo4j Database (NEO)

### 🚀 Deployment
#### NEO-1-001 Deploy standalone 🟢 ⬜
- NEO-2-001-EDT-01 Enterprise 🟢 ✅ 🧪
- NEO-2-001-LIC-01 Accepted license 🟢 ✅ 🧪
- NEO-2-001-LIC-02 Evaluation license 🟣 ✅
- NEO-2-001-MODE-01 Standalone 🟢 ✅ 🧪
#### NEO-1-002 Deploy cluster 🟢 ⬜
- NEO-2-002-CSZ-01 Minimum cluster size 🟢 ✅
- NEO-2-002-MODE-01 Cluster 🟢 ✅

### 🔧 Configuration
#### NEO-2-003 Runtime settings 🟢 ✅ 🧪
- NEO-3-003-APOC-01 APOC config 🟣 ✅
- NEO-3-003-APOC-02 APOC credentials 🟣 ⬜
- NEO-3-003-CFG-01 Core config 🟢 ✅ 🧪
- NEO-3-003-CFG-02 Strict validation toggle 🟣 ✅
- NEO-3-003-CFG-03 SPeeDy feature flag 🟣 ⬜
- NEO-3-003-JVM-01 Default JVM arguments 🟢 ⚠️
- NEO-3-003-JVM-02 Additional JVM arguments 🟣 ✅ 🧪

### 🔐 Security
#### NEO-2-004 Authentication and secrets 🟢 ✅ 🧪
- NEO-3-004-CRED-01 Generated password 🟢 ✅ 🧪
- NEO-3-004-CRED-02 Password from Secret 🟢 ✅ 🧪
- NEO-3-004-IMG-01 Existing pull secret 🟢 ✅
- NEO-3-004-IMG-02 Create image credential secret 🟣 ⬜
- NEO-3-004-SEC-01 Secret mounts 🟣 ⬜
- NEO-3-004-SEC-02 LDAP password secret 🟣 ⬜
#### NEO-2-005 TLS / SSL 🟢 ⬜
- NEO-3-005-TLS-01 Bolt TLS 🟣 ✅
- NEO-3-005-TLS-02 HTTPS TLS 🟣 ✅
- NEO-3-005-TLS-03 Cluster TLS 🟢 ✅
- NEO-3-005-TLS-04 TLS reload 🟣 ⬜

### 💾 Storage
#### NEO-2-006 Persistent volumes 🟢 ✅
- NEO-3-006-CLD-01 Cloud storage w/ Workload Identity 🟣 ⬜
- NEO-3-006-CLD-02 Cloud storage w/o Workload Identity 🟣 ⬜
- NEO-3-006-PVC-01 Default StorageClass 🟣 ⬜
- NEO-3-006-PVC-02 Existing StorageClass 🟢 ✅
- NEO-3-006-PVC-03 Existing PVC 🟣 ✅
- NEO-3-006-PVC-04 Selector 🟣 ✅
- NEO-3-006-PVC-05 VolumeClaimTemplate 🟣 ✅
- NEO-3-006-VOL-01 Data 🟢 ✅ 🧪
- NEO-3-006-VOL-02 Backups 🟣 ✅
- NEO-3-006-VOL-03 Logs 🟣 ✅
- NEO-3-006-VOL-04 Metrics 🟣 ✅
- NEO-3-006-VOL-05 Import 🟣 ✅
- NEO-3-006-VOL-06 Licenses 🟣 ✅

### 🌐 Networking
#### NEO-2-007 Expose services 🟢 ✅
- NEO-3-007-MULTI-01 Multi-cluster disabled 🟢 ✅
- NEO-3-007-MULTI-02 Multi-cluster enabled 🟣 ⬜
- NEO-3-007-PCMB-01 Bolt only 🟣 ⬜
- NEO-3-007-PCMB-02 HTTPS only 🟣 ⬜
- NEO-3-007-PCMB-03 HTTP + Bolt 🟢 ✅ 🧪
- NEO-3-007-PCMB-04 HTTPS + Bolt 🟣 ✅
- NEO-3-007-PCMB-05 HTTP + HTTPS + Bolt 🟣 ✅
- NEO-3-007-PCMB-06 HTTP + HTTPS 🟣 ⬜
- NEO-3-007-PRT-01 HTTP 🟢 ✅ 🧪
- NEO-3-007-PRT-02 HTTPS 🟣 ✅
- NEO-3-007-PRT-03 Bolt 🟢 ✅ 🧪
- NEO-3-007-PRT-04 Backup 🟣 ⬜
- NEO-3-007-SVC-01 ClusterIP 🟢 ✅ 🧪
- NEO-3-007-SVC-02 NodePort 🟣 ✅
- NEO-3-007-SVC-03 LoadBalancer 🟣 ✅

### 📌 Scheduling
#### NEO-2-008 Pod scheduling 🟣 ✅
- NEO-3-008-SCH-01 Node selector 🟣 ✅
- NEO-3-008-SCH-02 Affinity / anti-affinity 🟣 ✅
- NEO-3-008-SCH-03 Tolerations 🟣 ✅
- NEO-3-008-SCH-04 Topology spread 🟣 ⬜
- NEO-3-008-SCH-05 Priority class 🟣 ✅
- NEO-3-008-SCH-06 Service account 🟣 ⬜

### ❤️ Health
#### NEO-2-009 Health probes 🟢 ✅
- NEO-3-009-PROBE-01 Default probes 🟢 ✅
- NEO-3-009-PROBE-02 Custom probes 🟣 ✅

### ♻️ Lifecycle
#### NEO-2-010 Apply config changes safely 🟢 ✅
- NEO-3-010-RSTR-01 Controlled restart 🟢 ✅ 🧪
- NEO-3-010-RSTR-02 Rolling restart 🟢 ✅
#### NEO-2-011 Scale cluster 🟢 ✅
- NEO-3-011-CSZ-01 Scale out/in 🟢 ✅
- NEO-3-011-SRV-01 Automatic enable server 🟢 ✅
#### NEO-2-012 Upgrade Neo4j version 🟣 ⬜
- NEO-3-012-UPG-01 Image upgrade 🟣 ⬜
- NEO-3-012-UPG-02 Preflight validation 🟣 ⬜

### 🗃️ Backup & Restore
#### NEO-2-013 Run backups 🟣 ⬜
- NEO-3-013-BKMD-01 On-demand 🟣 ⬜
- NEO-3-013-BKMD-02 Scheduled 🟣 ⬜
- NEO-3-013-BKST-01 Shared local volume 🟣 ⬜
- NEO-3-013-BKST-02 Dedicated PVC 🟣 ⬜
- NEO-3-013-BKST-03 Cloud object storage 🟣 ⬜
#### NEO-2-014 Restore from backup or seed 🟣 ⬜
- NEO-3-014-RSTM-01 Restore from backup volume 🟣 ⬜
- NEO-3-014-RSTM-02 Restore from seed URI 🟣 ⬜

### 📊 Observability
#### NEO-2-015 Expose metrics 🟣 ✅
- NEO-3-015-MON-01 Disabled 🟣 ✅
- NEO-3-015-MON-02 ServiceMonitor 🟣 ✅
#### NEO-2-016 Logging 🟣 ⬜
- NEO-3-016-LOG-01 Default logs 🟣 ✅
- NEO-3-016-LOG-02 Custom log config 🟣 ✅

### 🛠️ Maintenance
#### NEO-2-017 Maintenance operations 🟣 ⬜
- NEO-3-017-JOB-01 Dump/load 🟣 ⬜
- NEO-3-017-JOB-02 Consistency check 🟣 ⬜
- NEO-3-017-JOB-03 Backup verification 🟣 ⬜
- NEO-3-017-MNT-01 Offline maintenance 🟣 ✅

### 🧹 Uninstall
#### NEO-2-018 Uninstall Neo4j workload 🟢 ✅ 🧪
