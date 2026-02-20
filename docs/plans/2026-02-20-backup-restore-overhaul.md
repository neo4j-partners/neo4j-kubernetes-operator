# Backup & Restore Overhaul Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 24 identified correctness bugs and API design problems in the Neo4j backup/restore system so that backup and restore operations actually work end-to-end for both cluster and standalone deployments.

**Architecture:**
- Backup Jobs run `neo4j-admin database backup --from=<cluster-backup-addresses>:6362 --to-path=<destination>` inside the Neo4j enterprise image — eliminating the broken sidecar-exec pattern.
- Cloud storage uses Neo4j's native `--to-path=s3://...` / `gs://...` / `azb://...` support — no extra CLI tools.
- Restore Jobs run `neo4j-admin database restore --from-path=<path>` and the controller then executes `CREATE DATABASE` via Bolt after the Job succeeds.

**Tech Stack:** Go 1.24, controller-runtime 0.21, Ginkgo v2/Gomega, kubebuilder markers, `make manifests generate`, `make test-unit`

---

## Task 1 — Version helper: fix `GetBackupCommand` + add new version methods

**Files:**
- Modify: `internal/neo4j/version.go`
- Modify: `internal/neo4j/version_test.go`

**Context:** `GetBackupCommand` puts the database name _before_ `--to-path` (wrong order per docs). It also conflates "backup all databases" (`"*"` wildcard) with "include metadata" (`--include-metadata`). A new `--from` parameter is needed. `SupportsAdvancedBackupFlags` gates `--remote-address-resolution` (2025.09) at the wrong version (2025.11). `--prefer-diff-as-parent` (2025.04) is missing entirely.

**Step 1 — Write failing tests**

In `internal/neo4j/version_test.go`, add (or update) the following tests right after the existing `TestGetBackupCommand`:

```go
func TestGetBackupCommandArgumentOrder(t *testing.T) {
    v, _ := ParseVersion("5.26.0-enterprise")
    cmd := GetBackupCommand(v, "mydb", "/backups/mydb", false, "server-0:6362")
    // --to-path must come before the database name
    toPathIdx := strings.Index(cmd, "--to-path")
    dbIdx := strings.LastIndex(cmd, "mydb")
    if toPathIdx < 0 || dbIdx < 0 || toPathIdx > dbIdx {
        t.Errorf("--to-path must appear before database name, got: %q", cmd)
    }
}

func TestGetBackupCommandAllDatabases(t *testing.T) {
    v, _ := ParseVersion("5.26.0-enterprise")
    cmd := GetBackupCommand(v, "", "/backups/all", true, "server-0:6362")
    if !strings.Contains(cmd, `"*"`) {
        t.Errorf(`expected wildcard "*" for all-databases backup, got: %q`, cmd)
    }
    // --include-metadata should NOT appear in the base command (it's a separate option)
    if strings.Contains(cmd, "--include-metadata") {
        t.Errorf("--include-metadata should not be in base backup command, got: %q", cmd)
    }
}

func TestGetBackupCommandFromFlag(t *testing.T) {
    v, _ := ParseVersion("5.26.0-enterprise")
    cmd := GetBackupCommand(v, "mydb", "/backups/mydb", false, "host1:6362,host2:6362")
    if !strings.Contains(cmd, "--from=host1:6362,host2:6362") {
        t.Errorf("expected --from flag, got: %q", cmd)
    }
}

func TestSupportsRemoteAddressResolution(t *testing.T) {
    v509, _ := ParseVersion("2025.09.0-enterprise")
    v508, _ := ParseVersion("2025.08.0-enterprise")
    v526, _ := ParseVersion("5.26.0-enterprise")
    if !v509.SupportsRemoteAddressResolution() {
        t.Error("2025.09 should support --remote-address-resolution")
    }
    if v508.SupportsRemoteAddressResolution() {
        t.Error("2025.08 should not support --remote-address-resolution")
    }
    if v526.SupportsRemoteAddressResolution() {
        t.Error("5.26 should not support --remote-address-resolution")
    }
}

func TestSupportsPreferDiffAsParent(t *testing.T) {
    v504, _ := ParseVersion("2025.04.0-enterprise")
    v503, _ := ParseVersion("2025.03.0-enterprise")
    v526, _ := ParseVersion("5.26.0-enterprise")
    if !v504.SupportsPreferDiffAsParent() {
        t.Error("2025.04 should support --prefer-diff-as-parent")
    }
    if v503.SupportsPreferDiffAsParent() {
        t.Error("2025.03 should not support --prefer-diff-as-parent")
    }
    if v526.SupportsPreferDiffAsParent() {
        t.Error("5.26 should not support --prefer-diff-as-parent")
    }
}

func TestSupportsParallelDownload(t *testing.T) {
    v511, _ := ParseVersion("2025.11.0-enterprise")
    v510, _ := ParseVersion("2025.10.0-enterprise")
    if !v511.SupportsParallelDownload() {
        t.Error("2025.11 should support --parallel-download")
    }
    if v510.SupportsParallelDownload() {
        t.Error("2025.10 should not support --parallel-download")
    }
}
```

**Step 2 — Run to confirm failure**

```bash
go test ./internal/neo4j/... -run "TestGetBackupCommandArgumentOrder|TestGetBackupCommandAllDatabases|TestGetBackupCommandFromFlag|TestSupportsRemoteAddressResolution|TestSupportsPreferDiffAsParent|TestSupportsParallelDownload" -v
```

Expected: FAIL — wrong signature or missing methods.

**Step 3 — Implement in `internal/neo4j/version.go`**

Replace `GetBackupCommand` and add new methods:

```go
// GetBackupCommand generates the correct neo4j-admin database backup command.
// fromAddresses is a comma-separated list of host:port backup endpoints (port 6362).
// If fromAddresses is empty, the --from flag is omitted (local backup).
func GetBackupCommand(version *Version, databaseName string, backupPath string, allDatabases bool, fromAddresses string) string {
    cmd := "neo4j-admin database backup"

    if fromAddresses != "" {
        cmd += " --from=" + fromAddresses
    }
    cmd += " --to-path=" + backupPath

    if allDatabases {
        cmd += ` "*"`
    } else if databaseName != "" {
        cmd += " " + databaseName
    }

    return cmd
}

// SupportsRemoteAddressResolution reports whether --remote-address-resolution is available.
// Introduced in CalVer 2025.09.
func (v *Version) SupportsRemoteAddressResolution() bool {
    if !v.IsCalver {
        return false
    }
    if v.Major > 2025 {
        return true
    }
    return v.Major == 2025 && v.Minor >= 9
}

// SupportsParallelDownload reports whether --parallel-download is available.
// Introduced in CalVer 2025.11.
func (v *Version) SupportsParallelDownload() bool {
    if !v.IsCalver {
        return false
    }
    if v.Major > 2025 {
        return true
    }
    return v.Major == 2025 && v.Minor >= 11
}

// SupportsSkipRecovery reports whether --skip-recovery is available.
// Introduced in CalVer 2025.11 (same as parallel-download).
func (v *Version) SupportsSkipRecovery() bool {
    return v.SupportsParallelDownload()
}

// SupportsPreferDiffAsParent reports whether --prefer-diff-as-parent is available.
// Introduced in CalVer 2025.04.
func (v *Version) SupportsPreferDiffAsParent() bool {
    if !v.IsCalver {
        return false
    }
    if v.Major > 2025 {
        return true
    }
    return v.Major == 2025 && v.Minor >= 4
}

// SupportsAdvancedBackupFlags is kept for backward compatibility.
// Prefer the specific SupportsXxx methods.
// Deprecated: use SupportsParallelDownload / SupportsRemoteAddressResolution / SupportsSkipRecovery.
func (v *Version) SupportsAdvancedBackupFlags() bool {
    return v.SupportsParallelDownload()
}
```

Also update the call sites in `internal/neo4j/version_test.go` for `TestGetBackupCommand` and `TestGetRestoreCommand` to pass the new `fromAddresses` argument: `GetBackupCommand(v, "mydb", "/backups/mydb", false, "")`.

**Step 4 — Fix `TestSupportsAdvancedBackupFlags`** in `version_test.go` — the existing test for `SupportsAdvancedBackupFlags` with `2025.01` should still pass (returns false), and `2025.11` should still return true. No changes needed if the deprecated wrapper delegates correctly.

**Step 5 — Run tests**

```bash
go test ./internal/neo4j/... -v
```

Expected: all PASS.

**Step 6 — Fix callers of old `GetBackupCommand` signature**

```bash
grep -rn "GetBackupCommand(" internal/ --include="*.go"
```

Update `internal/controller/neo4jbackup_controller.go` lines 567/570/573 to pass an empty `fromAddresses` string (temporary; proper value set in Task 6). Update any test callers in `test/integration/version_detection_test.go` similarly.

**Step 7 — Commit**

```bash
git add internal/neo4j/version.go internal/neo4j/version_test.go test/integration/version_detection_test.go internal/controller/neo4jbackup_controller.go
git commit -m "fix(backup): fix GetBackupCommand arg order, add per-flag version methods"
```

---

## Task 2 — API types: add `ClusterRef`, `PreferDiffAsParent`, `TempPath`, `CredentialsSecretRef`

**Files:**
- Modify: `api/v1alpha1/neo4jbackup_types.go`
- Modify: `api/v1alpha1/neo4jenterprisecluster_types.go` (StorageLocation / CloudBlock)
- Run: `make manifests generate`

**Context:** `BackupTarget` has no cluster reference when `Kind="Database"`. `BackupOptions` is missing `PreferDiffAsParent` and `TempPath`. `CloudBlock` has no way to reference a Kubernetes Secret for credentials. These additions are backwards-compatible (all new fields are optional).

**Step 1 — Add `ClusterRef` to `BackupTarget`**

In `api/v1alpha1/neo4jbackup_types.go`, update `BackupTarget`:

```go
type BackupTarget struct {
    // +kubebuilder:validation:Enum=Cluster;Database
    // +kubebuilder:validation:Required
    Kind string `json:"kind"`

    // Name of the target resource.
    // When Kind=Cluster: name of the Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone.
    // When Kind=Database: name of the database to back up (ClusterRef must also be set).
    // +kubebuilder:validation:Required
    Name string `json:"name"`

    // ClusterRef is the name of the cluster (Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone)
    // that owns the database. Required when Kind=Database.
    ClusterRef string `json:"clusterRef,omitempty"`

    // Namespace of the target resource (defaults to backup namespace).
    Namespace string `json:"namespace,omitempty"`
}
```

**Step 2 — Add `PreferDiffAsParent` and `TempPath` to `BackupOptions`**

In `api/v1alpha1/neo4jbackup_types.go`, add to `BackupOptions`:

```go
    // PreferDiffAsParent uses the latest differential backup as parent instead of
    // the latest backup when performing a differential backup.
    // Requires CalVer 2025.04+. See --prefer-diff-as-parent in neo4j-admin.
    PreferDiffAsParent bool `json:"preferDiffAsParent,omitempty"`

    // TempPath is a path for temporary files during backup-related commands.
    // Strongly recommended when backing up to cloud storage.
    // Maps to --temp-path in neo4j-admin.
    TempPath string `json:"tempPath,omitempty"`
```

**Step 3 — Add `CredentialsSecretRef` to `CloudBlock`**

In `api/v1alpha1/neo4jenterprisecluster_types.go`, add to `CloudBlock`:

```go
type CloudBlock struct {
    // +kubebuilder:validation:Enum=aws;gcp;azure
    Provider string `json:"provider,omitempty"`

    Identity *CloudIdentity `json:"identity,omitempty"`

    // CredentialsSecretRef is the name of a Kubernetes Secret containing
    // cloud provider credentials as environment variables.
    // For S3: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION (optional: AWS_ENDPOINT_URL).
    // For GCS: GOOGLE_APPLICATION_CREDENTIALS_JSON (base64-encoded service account key JSON).
    // For Azure: AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY.
    CredentialsSecretRef string `json:"credentialsSecretRef,omitempty"`
}
```

**Step 4 — Regenerate CRDs**

```bash
make manifests generate
```

Expected: `config/crd/bases/neo4j.neo4j.com_neo4jbackups.yaml` and `charts/neo4j-operator/crds/` updated with new fields.

**Step 5 — Verify no compile errors**

```bash
make build
```

Expected: success.

**Step 6 — Commit**

```bash
git add api/v1alpha1/ config/crd/ charts/neo4j-operator/crds/ bundle/manifests/
git commit -m "feat(api): add ClusterRef, PreferDiffAsParent, TempPath, CredentialsSecretRef to backup types"
```

---

## Task 3 — Cluster config: enable backup service for remote access

**Files:**
- Modify: `internal/resources/cluster.go` (function `buildNeo4jConfigForEnterprise`)
- Modify: `internal/resources/cluster_test.go`

**Context:** `server.backup.listen_address` defaults to `127.0.0.1:6362`, making the backup port unreachable from a separate backup Job pod. It must be set to `0.0.0.0:6362`. The backup port (6362) is already exposed as a container port — this change makes the service actually reachable.

**Step 1 — Write failing test**

In `internal/resources/cluster_test.go`, add:

```go
func TestClusterConfigContainsBackupListenAddress(t *testing.T) {
    cluster := buildMinimalTestCluster("backup-listen-test")
    config := buildNeo4jConfigForEnterprise(cluster)
    assert.Contains(t, config, "server.backup.listen_address=0.0.0.0:6362",
        "backup listen address must be reachable from other pods")
}
```

**Step 2 — Run to confirm failure**

```bash
go test ./internal/resources/... -run TestClusterConfigContainsBackupListenAddress -v
```

Expected: FAIL.

**Step 3 — Add setting to config**

In `internal/resources/cluster.go`, in `buildNeo4jConfigForEnterprise`, within the server settings block (around line 1531 after `server.cluster.listen_address`):

```go
server.backup.enabled=true
server.backup.listen_address=0.0.0.0:6362
```

**Step 4 — Run test**

```bash
go test ./internal/resources/... -run TestClusterConfigContainsBackupListenAddress -v
```

Expected: PASS.

**Step 5 — Commit**

```bash
git add internal/resources/cluster.go internal/resources/cluster_test.go
git commit -m "fix(config): enable backup service on 0.0.0.0:6362 so backup Jobs can connect"
```

---

## Task 4 — Backup helper: `buildBackupFromAddresses` utility

**Files:**
- Modify: `internal/resources/cluster.go` (add helper)
- Modify: `internal/resources/cluster_test.go`

**Context:** Backup Jobs need `--from=server-0.headless:6362,server-1.headless:6362,...` The logic to construct these addresses belongs near the existing FQDN-building code.

**Step 1 — Write failing test**

```go
func TestBuildBackupFromAddresses(t *testing.T) {
    cluster := buildMinimalTestCluster("my-cluster")
    cluster.Spec.Topology.Servers = 3
    cluster.Namespace = "default"
    addrs := BuildBackupFromAddresses(cluster)
    expected := "my-cluster-server-0.my-cluster-headless.default.svc.cluster.local:6362," +
        "my-cluster-server-1.my-cluster-headless.default.svc.cluster.local:6362," +
        "my-cluster-server-2.my-cluster-headless.default.svc.cluster.local:6362"
    assert.Equal(t, expected, addrs)
}
```

**Step 2 — Run to confirm failure**

```bash
go test ./internal/resources/... -run TestBuildBackupFromAddresses -v
```

Expected: FAIL — function not defined.

**Step 3 — Implement in `internal/resources/cluster.go`**

```go
// BuildBackupFromAddresses returns a comma-separated list of
// "pod-fqdn:6362" addresses for all server pods, suitable for use as
// the --from flag of neo4j-admin database backup.
func BuildBackupFromAddresses(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
    servers := int(cluster.Spec.Topology.Servers)
    addrs := make([]string, servers)
    for i := 0; i < servers; i++ {
        addrs[i] = fmt.Sprintf("%s-server-%d.%s-headless.%s.svc.cluster.local:%d",
            cluster.Name, i, cluster.Name, cluster.Namespace, BackupPort)
    }
    return strings.Join(addrs, ",")
}
```

**Step 4 — Run test**

```bash
go test ./internal/resources/... -run TestBuildBackupFromAddresses -v
```

Expected: PASS.

**Step 5 — Commit**

```bash
git add internal/resources/cluster.go internal/resources/cluster_test.go
git commit -m "feat(backup): add BuildBackupFromAddresses helper for backup Job --from flag"
```

---

## Task 5 — Backup controller: fix `isClusterReady` and `getTargetCluster`

**Files:**
- Modify: `internal/controller/neo4jbackup_controller.go`
- Modify: `internal/controller/neo4jbackup_controller_test.go`

**Context:**
1. `isClusterReady` checks Conditions instead of `status.phase` — unreliable per CLAUDE.md item 15.
2. `getTargetCluster` with `Kind="Database"` uses the database name as a cluster name — always fails.
3. Only `Neo4jEnterpriseCluster` is supported — standalone not attempted.

**Step 1 — Write failing tests**

In `internal/controller/neo4jbackup_controller_test.go`:

```go
It("should use ClusterRef when Kind=Database", func() {
    cluster := buildTestCluster("my-cluster", testNamespace)
    Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

    backup := &neo4jv1alpha1.Neo4jBackup{
        ObjectMeta: metav1.ObjectMeta{Name: "db-backup", Namespace: testNamespace},
        Spec: neo4jv1alpha1.Neo4jBackupSpec{
            Target: neo4jv1alpha1.BackupTarget{
                Kind:       "Database",
                Name:       "neo4j",         // database name
                ClusterRef: "my-cluster",    // cluster ref
            },
            Storage: neo4jv1alpha1.StorageLocation{Type: "pvc"},
        },
    }
    Expect(k8sClient.Create(ctx, backup)).To(Succeed())

    // Reconcile once — should NOT fail with "cluster not found"
    result, err := reconciler.Reconcile(ctx, ctrl.Request{
        NamespacedName: types.NamespacedName{Name: "db-backup", Namespace: testNamespace},
    })
    Expect(err).NotTo(HaveOccurred())
    _ = result
})
```

**Step 2 — Run to confirm failure**

```bash
go test ./internal/controller/... -run "should use ClusterRef" -v
```

**Step 3 — Fix `getTargetCluster`**

```go
func (r *Neo4jBackupReconciler) getTargetCluster(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
    targetNamespace := backup.Spec.Target.Namespace
    if targetNamespace == "" {
        targetNamespace = backup.Namespace
    }

    // Determine the cluster name: for Kind=Database use ClusterRef; for Kind=Cluster use Name.
    clusterName := backup.Spec.Target.Name
    if backup.Spec.Target.Kind == "Database" {
        if backup.Spec.Target.ClusterRef == "" {
            return nil, fmt.Errorf("ClusterRef must be set when backup target Kind is Database")
        }
        clusterName = backup.Spec.Target.ClusterRef
    }

    // Try Neo4jEnterpriseCluster first
    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
    if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, cluster); err == nil {
        return cluster, nil
    }

    // Fall back: try Neo4jEnterpriseStandalone (wrap it in a synthetic cluster object with same fields)
    standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
    if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, standalone); err != nil {
        return nil, fmt.Errorf("target %q not found as Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone: %w", clusterName, err)
    }
    return standaloneToCluster(standalone), nil
}
```

Add the `standaloneToCluster` helper (creates a synthetic `Neo4jEnterpriseCluster` with the same image and auth from the standalone, used only for image/version/auth lookups — not reconciled). It should populate: `Spec.Image`, `Spec.Auth`, `Name`, `Namespace`, `Status.Phase`.

**Step 4 — Fix `isClusterReady`**

```go
func (r *Neo4jBackupReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
    return cluster.Status.Phase == "Ready"
}
```

**Step 5 — Run tests**

```bash
go test ./internal/controller/... -run "Neo4jBackup" -v
```

Expected: PASS.

**Step 6 — Commit**

```bash
git add internal/controller/neo4jbackup_controller.go internal/controller/neo4jbackup_controller_test.go
git commit -m "fix(backup): fix isClusterReady (use phase), fix getTargetCluster for Kind=Database and standalone"
```

---

## Task 6 — Backup controller: replace sidecar-exec pattern with direct `neo4j-admin` Job

**Files:**
- Modify: `internal/controller/neo4jbackup_controller.go`
- Modify: `internal/controller/neo4jbackup_controller_test.go`

**Context:** The current `createBackupJob` and `createBackupCronJob` create Jobs that exec into a non-existent `backup-sidecar` container. These must be replaced with Jobs that run `neo4j-admin database backup` directly in a container using the cluster's Neo4j image. The `buildBackupCommand` method already exists but was never called — it now becomes the source of truth. Cloud storage must use `--to-path=s3://...` natively instead of uploading after the fact.

**Step 1 — Write failing tests for backup Job structure**

In `internal/controller/neo4jbackup_controller_test.go`, replace the existing tests that assert `ContainSubstring("backup-sidecar")` with:

```go
It("backup Job should use neo4j image and run neo4j-admin directly", func() {
    // ... setup cluster + backup ...
    job, err := reconciler.createBackupJob(ctx, backup, cluster)
    Expect(err).NotTo(HaveOccurred())
    container := job.Spec.Template.Spec.Containers[0]
    Expect(container.Image).To(ContainSubstring("neo4j:"))
    Expect(container.Args[1]).To(ContainSubstring("neo4j-admin database backup"))
    Expect(container.Args[1]).NotTo(ContainSubstring("backup-sidecar"))
    Expect(container.Args[1]).To(ContainSubstring("--from="))
    Expect(container.Args[1]).To(ContainSubstring("--to-path="))
})

It("backup Job for cloud storage should use native cloud URI in --to-path", func() {
    // ... setup cluster + S3 backup ...
    job, err := reconciler.createBackupJob(ctx, backup, cluster)
    Expect(err).NotTo(HaveOccurred())
    container := job.Spec.Template.Spec.Containers[0]
    Expect(container.Args[1]).To(ContainSubstring("--to-path=s3://"))
    Expect(container.Args[1]).NotTo(ContainSubstring("aws s3 cp"))
})

It("backup Job for PVC storage should mount the PVC", func() {
    // ... setup cluster + PVC backup ...
    job, err := reconciler.createBackupJob(ctx, backup, cluster)
    Expect(err).NotTo(HaveOccurred())
    Expect(job.Spec.Template.Spec.Volumes).To(ContainElement(
        MatchFields(IgnoreExtras, Fields{
            "Name": Equal("backup-storage"),
            "VolumeSource": MatchFields(IgnoreExtras, Fields{
                "PersistentVolumeClaim": Not(BeNil()),
            }),
        }),
    ))
})
```

**Step 2 — Run to confirm failure**

```bash
go test ./internal/controller/... -run "backup Job" -v
```

**Step 3 — Rewrite `createBackupJob`**

Replace the entire function body. Key changes:

```go
func (r *Neo4jBackupReconciler) createBackupJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.Job, error) {
    jobName := backup.Name + "-backup"

    // Build neo4j-admin command (now actually used)
    backupCmd, err := r.buildBackupCommand(backup, cluster)
    if err != nil {
        return nil, fmt.Errorf("failed to build backup command: %w", err)
    }

    image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
    backoffLimit := int32(3)

    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      jobName,
            Namespace: backup.Namespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":       "neo4j-backup",
                "app.kubernetes.io/instance":   backup.Name,
                "app.kubernetes.io/component":  "backup",
                "app.kubernetes.io/managed-by": "neo4j-operator",
            },
        },
        Spec: batchv1.JobSpec{
            BackoffLimit: &backoffLimit,
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyNever,
                    Containers: []corev1.Container{
                        {
                            Name:         "backup",
                            Image:        image,
                            Command:      []string{"/bin/sh"},
                            Args:         []string{"-c", backupCmd},
                            Env:          r.buildCloudEnvVars(backup),
                            VolumeMounts: r.buildVolumeMounts(backup),
                        },
                    },
                    Volumes: r.buildVolumes(backup),
                },
            },
        },
    }

    if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
        return nil, err
    }
    if err := r.Create(ctx, job); err != nil {
        return nil, err
    }
    return job, nil
}
```

**Step 4 — Rewrite `buildBackupCommand`**

The method now actually builds the command that the Job will run. Connect it to the `GetBackupCommand` helper with a proper `--from` address:

```go
func (r *Neo4jBackupReconciler) buildBackupCommand(backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (string, error) {
    imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
    version, err := neo4j.GetImageVersion(imageTag)
    if err != nil {
        version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
    }

    // Validate version-gated flags
    if backup.Spec.Options != nil {
        if backup.Spec.Options.ParallelDownload && !version.SupportsParallelDownload() {
            return "", fmt.Errorf("--parallel-download requires CalVer 2025.11+ (got %s)", cluster.Spec.Image.Tag)
        }
        if backup.Spec.Options.RemoteAddressResolution && !version.SupportsRemoteAddressResolution() {
            return "", fmt.Errorf("--remote-address-resolution requires CalVer 2025.09+ (got %s)", cluster.Spec.Image.Tag)
        }
        if backup.Spec.Options.SkipRecovery && !version.SupportsSkipRecovery() {
            return "", fmt.Errorf("--skip-recovery requires CalVer 2025.11+ (got %s)", cluster.Spec.Image.Tag)
        }
        if backup.Spec.Options.PreferDiffAsParent && !version.SupportsPreferDiffAsParent() {
            return "", fmt.Errorf("--prefer-diff-as-parent requires CalVer 2025.04+ (got %s)", cluster.Spec.Image.Tag)
        }
    }

    // Determine backup destination path / URI
    toPath := r.buildToPath(backup)

    // Determine --from addresses
    fromAddresses := resources.BuildBackupFromAddresses(cluster)

    // Determine database argument
    allDatabases := backup.Spec.Target.Kind == "Cluster"
    dbName := ""
    if !allDatabases {
        dbName = backup.Spec.Target.Name // Kind=Database: Name is the database name
    }

    cmd := neo4j.GetBackupCommand(version, dbName, toPath, allDatabases, fromAddresses)

    // Type (FULL / DIFF / AUTO)
    if backup.Spec.Options != nil && backup.Spec.Options.BackupType != "" {
        cmd += " --type=" + backup.Spec.Options.BackupType
    }

    // Compression: only add --compress=false if explicitly disabled
    if backup.Spec.Options != nil && backup.Spec.Options.Compress != nil && !*backup.Spec.Options.Compress {
        cmd += " --compress=false"
    }

    // Page cache
    if backup.Spec.Options != nil && backup.Spec.Options.PageCache != "" {
        cmd += " --pagecache=" + backup.Spec.Options.PageCache
    }

    // Temp path (strongly recommended for cloud)
    if backup.Spec.Options != nil && backup.Spec.Options.TempPath != "" {
        cmd += " --temp-path=" + backup.Spec.Options.TempPath
    }

    // Version-gated flags
    if backup.Spec.Options != nil {
        if backup.Spec.Options.PreferDiffAsParent {
            cmd += " --prefer-diff-as-parent"
        }
        if backup.Spec.Options.RemoteAddressResolution {
            cmd += " --remote-address-resolution=true"
        }
        if backup.Spec.Options.ParallelDownload {
            cmd += " --parallel-download=true"
        }
        if backup.Spec.Options.SkipRecovery {
            cmd += " --skip-recovery=true"
        }
    }

    // Additional args
    if backup.Spec.Options != nil {
        for _, arg := range backup.Spec.Options.AdditionalArgs {
            cmd += " " + arg
        }
    }

    // For PVC storage: ensure target directory exists
    if backup.Spec.Storage.Type == "pvc" {
        backupDir := toPath
        cmd = fmt.Sprintf("mkdir -p %s && %s", backupDir, cmd)
    }

    return cmd, nil
}

// buildToPath returns the --to-path value (local path or cloud URI).
func (r *Neo4jBackupReconciler) buildToPath(backup *neo4jv1alpha1.Neo4jBackup) string {
    switch backup.Spec.Storage.Type {
    case "s3":
        path := backup.Spec.Storage.Path
        if path == "" {
            path = "backups"
        }
        return fmt.Sprintf("s3://%s/%s/", backup.Spec.Storage.Bucket, path)
    case "gcs":
        path := backup.Spec.Storage.Path
        if path == "" {
            path = "backups"
        }
        return fmt.Sprintf("gs://%s/%s/", backup.Spec.Storage.Bucket, path)
    case "azure":
        path := backup.Spec.Storage.Path
        if path == "" {
            path = "backups"
        }
        return fmt.Sprintf("azb://%s/%s/", backup.Spec.Storage.Bucket, path)
    default: // pvc
        backupName := fmt.Sprintf("%s-%s", backup.Name, time.Now().Format("20060102-150405"))
        return fmt.Sprintf("/backup/%s", backupName)
    }
}
```

**Note on `Compress *bool`:** In Task 2, `Compress` type was changed to `*bool`. If not yet done as part of Task 2, update the conditional in `buildBackupCommand` accordingly. If keeping `bool`, use: `if backup.Spec.Options != nil && !backup.Spec.Options.Compress { cmd += " --compress=false" }` — but note this incorrectly disables compression when field is unset (see Issue #13). The `*bool` approach is cleaner.

**Step 5 — Add `buildCloudEnvVars` helper**

```go
// buildCloudEnvVars returns env vars for cloud credentials from CredentialsSecretRef.
func (r *Neo4jBackupReconciler) buildCloudEnvVars(backup *neo4jv1alpha1.Neo4jBackup) []corev1.EnvVar {
    var cloud *neo4jv1alpha1.CloudBlock
    if backup.Spec.Storage.Cloud != nil {
        cloud = backup.Spec.Storage.Cloud
    } else if backup.Spec.Cloud != nil {
        cloud = backup.Spec.Cloud
    }
    if cloud == nil || cloud.CredentialsSecretRef == "" {
        return nil
    }

    secretRef := cloud.CredentialsSecretRef
    // Inject all keys from the secret as env vars using envFrom
    // (handled by mounting via EnvFrom below; this method returns per-key mappings
    //  for known credential env var names)
    switch cloud.Provider {
    case "aws":
        return []corev1.EnvVar{
            {Name: "AWS_ACCESS_KEY_ID", ValueFrom: secretEnvSource(secretRef, "AWS_ACCESS_KEY_ID")},
            {Name: "AWS_SECRET_ACCESS_KEY", ValueFrom: secretEnvSource(secretRef, "AWS_SECRET_ACCESS_KEY")},
            {Name: "AWS_DEFAULT_REGION", ValueFrom: secretEnvSource(secretRef, "AWS_DEFAULT_REGION")},
        }
    case "gcp":
        return []corev1.EnvVar{
            {Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/var/secrets/gcp/credentials.json"},
        }
    case "azure":
        return []corev1.EnvVar{
            {Name: "AZURE_STORAGE_ACCOUNT", ValueFrom: secretEnvSource(secretRef, "AZURE_STORAGE_ACCOUNT")},
            {Name: "AZURE_STORAGE_KEY", ValueFrom: secretEnvSource(secretRef, "AZURE_STORAGE_KEY")},
        }
    }
    return nil
}

func secretEnvSource(secretName, key string) *corev1.EnvVarSource {
    return &corev1.EnvVarSource{
        SecretKeyRef: &corev1.SecretKeySelector{
            LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
            Key:                  key,
        },
    }
}
```

Also update `buildVolumeMounts` and `buildVolumes` to add a GCP credentials volume when `cloud.Provider == "gcp"`.

**Step 6 — Remove the old `buildCloudUploadCommand` method** (it's now superseded by native `--to-path` support). Delete it.

**Step 7 — Apply the same changes to `createBackupCronJob`** — replace the inline sidecar script with a reference to `buildBackupCommand`. The CronJob job template should use the same container spec as `createBackupJob`.

**Step 8 — Run all backup controller tests**

```bash
go test ./internal/controller/... -run "Neo4jBackup" -v
```

Expected: all PASS.

**Step 9 — Commit**

```bash
git add internal/controller/neo4jbackup_controller.go internal/controller/neo4jbackup_controller_test.go
git commit -m "fix(backup): replace sidecar-exec pattern with direct neo4j-admin Job, native cloud --to-path"
```

---

## Task 7 — Backup controller: fix CronJob update + add `--prefer-diff-as-parent`

**Files:**
- Modify: `internal/controller/neo4jbackup_controller.go`

**Context:** If a backup's schedule, type, or options change, the existing CronJob is silently never updated. The function returns early without calling `r.Update`.

**Step 1 — Write failing test**

```go
It("should update CronJob when backup schedule changes", func() {
    // create cluster + scheduled backup, reconcile, then change schedule, reconcile again
    // verify CronJob has updated schedule
    cronJob := &batchv1.CronJob{}
    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: backup.Name + "-backup-cron", Namespace: testNamespace}, cronJob)).To(Succeed())
    Expect(cronJob.Spec.Schedule).To(Equal("0 2 * * *"))

    // Update backup schedule
    backup.Spec.Schedule = "0 3 * * *"
    Expect(k8sClient.Update(ctx, backup)).To(Succeed())
    // Reconcile
    _, err := reconciler.Reconcile(ctx, ...)
    Expect(err).NotTo(HaveOccurred())

    Expect(k8sClient.Get(ctx, ..., cronJob)).To(Succeed())
    Expect(cronJob.Spec.Schedule).To(Equal("0 3 * * *"))
})
```

**Step 2 — Fix `createBackupCronJob`**

Replace the `if err == nil { return existingCronJob, nil }` block with proper update logic using `controllerutil.CreateOrUpdate`:

```go
func (r *Neo4jBackupReconciler) createBackupCronJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.CronJob, error) {
    cronJobName := backup.Name + "-backup-cron"

    backupCmd, err := r.buildBackupCommand(backup, cluster)
    if err != nil {
        return nil, fmt.Errorf("failed to build backup command: %w", err)
    }

    image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
    backoffLimit := int32(3)

    cronJob := &batchv1.CronJob{
        ObjectMeta: metav1.ObjectMeta{
            Name:      cronJobName,
            Namespace: backup.Namespace,
        },
    }

    _, err = controllerutil.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
        cronJob.Labels = map[string]string{
            "app.kubernetes.io/name":       "neo4j-backup",
            "app.kubernetes.io/instance":   backup.Name,
            "app.kubernetes.io/component":  "backup-cron",
            "app.kubernetes.io/managed-by": "neo4j-operator",
        }
        cronJob.Spec.Schedule = backup.Spec.Schedule
        cronJob.Spec.JobTemplate = batchv1.JobTemplateSpec{
            Spec: batchv1.JobSpec{
                BackoffLimit: &backoffLimit,
                Template: corev1.PodTemplateSpec{
                    Spec: corev1.PodSpec{
                        RestartPolicy: corev1.RestartPolicyNever,
                        Containers: []corev1.Container{
                            {
                                Name:         "backup",
                                Image:        image,
                                Command:      []string{"/bin/sh"},
                                Args:         []string{"-c", backupCmd},
                                Env:          r.buildCloudEnvVars(backup),
                                VolumeMounts: r.buildVolumeMounts(backup),
                            },
                        },
                        Volumes: r.buildVolumes(backup),
                    },
                },
            },
        }
        return controllerutil.SetControllerReference(backup, cronJob, r.Scheme)
    })
    return cronJob, err
}
```

**Step 3 — Run tests**

```bash
go test ./internal/controller/... -run "CronJob" -v
```

Expected: PASS.

**Step 4 — Commit**

```bash
git add internal/controller/neo4jbackup_controller.go
git commit -m "fix(backup): update CronJob when backup spec changes using CreateOrUpdate"
```

---

## Task 8 — Restore controller: fix `stopCluster` / `startCluster` StatefulSet name

**Files:**
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/neo4jrestore_controller_test.go`

**Context:** In the server-based architecture, the StatefulSet is named `{cluster-name}-server`, not `{cluster-name}`. Both `stopCluster` and `startCluster` use the wrong name, causing a not-found error. `waitForClusterReady` uses an incorrect label selector.

**Step 1 — Write failing test**

```go
It("stopCluster should scale down the server StatefulSet", func() {
    sts := &appsv1.StatefulSet{}
    stsName := cluster.Name + "-server"  // CORRECT name
    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: testNamespace}, sts)).To(Succeed())

    err := reconciler.stopCluster(ctx, cluster)
    Expect(err).NotTo(HaveOccurred())

    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: testNamespace}, sts)).To(Succeed())
    Expect(*sts.Spec.Replicas).To(Equal(int32(0)))
})
```

**Step 2 — Fix `stopCluster`**

```go
func (r *Neo4jRestoreReconciler) stopCluster(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
    stsKey := types.NamespacedName{
        Name:      cluster.Name + "-server",   // Fixed: server-based StatefulSet name
        Namespace: cluster.Namespace,
    }
    // ... rest of the function unchanged
```

**Step 3 — Fix `startCluster`**

Same one-line fix: `Name: cluster.Name + "-server"`.

**Step 4 — Fix `waitForClusterReady` pod label selector**

The server pods are labeled with `app.kubernetes.io/instance: {cluster-name}-server`, not just `{cluster-name}`. Update the label selector:

```go
client.MatchingLabels{
    "neo4j.com/cluster": cluster.Name,   // Use the cluster label set by the cluster controller
}
```

Check `internal/resources/cluster.go` for the actual pod labels set on the StatefulSet pod template and use those exact keys.

**Step 5 — Run tests**

```bash
go test ./internal/controller/... -run "Neo4jRestore" -v
```

Expected: PASS.

**Step 6 — Commit**

```bash
git add internal/controller/neo4jrestore_controller.go internal/controller/neo4jrestore_controller_test.go
git commit -m "fix(restore): fix StatefulSet name in stopCluster/startCluster to cluster.Name+'-server'"
```

---

## Task 9 — Restore controller: fix `validateRestore` for all source types

**Files:**
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/neo4jrestore_controller_test.go`

**Context:** `validateRestore` handles only `"backup"` and `"storage"` — hitting `default: return error` for `"pitr"`, `"s3"`, and `"gcs"`. All PITR restores fail validation before starting. The `SourceTypeS3` and `SourceTypeGCS` constants exist but are unused in the switch.

**Step 1 — Write failing test**

```go
It("should accept pitr source type as valid", func() {
    restore := buildTestRestore("pitr-restore", testNamespace)
    restore.Spec.Source.Type = "pitr"
    restore.Spec.Source.PITR = &neo4jv1alpha1.PITRConfig{
        BaseBackup: &neo4jv1alpha1.BaseBackupSource{
            Type:       "storage",
            BackupPath: "/backups/neo4j-full.backup",
        },
    }
    restore.Spec.Source.PointInTime = &metav1.Time{Time: time.Now()}

    err := reconciler.validateRestore(ctx, restore)
    Expect(err).NotTo(HaveOccurred())
})
```

**Step 2 — Fix `validateRestore`**

```go
func (r *Neo4jRestoreReconciler) validateRestore(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) error {
    switch restore.Spec.Source.Type {
    case "backup":
        if restore.Spec.Source.BackupRef == "" {
            return fmt.Errorf("backupRef is required when source type is 'backup'")
        }
        backup := &neo4jv1alpha1.Neo4jBackup{}
        if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.Source.BackupRef, Namespace: restore.Namespace}, backup); err != nil {
            return fmt.Errorf("backup %q not found: %w", restore.Spec.Source.BackupRef, err)
        }

    case "storage", "s3", "gcs", "azure":
        if restore.Spec.Source.BackupPath == "" {
            return fmt.Errorf("backupPath is required when source type is %q", restore.Spec.Source.Type)
        }

    case "pitr":
        if restore.Spec.Source.PITR == nil {
            return fmt.Errorf("pitr configuration is required when source type is 'pitr'")
        }
        if restore.Spec.Source.PITR.BaseBackup == nil && restore.Spec.Source.PointInTime == nil {
            return fmt.Errorf("pitr requires either a baseBackup or a pointInTime (or both)")
        }

    default:
        return fmt.Errorf("invalid source type %q: must be one of backup, storage, pitr", restore.Spec.Source.Type)
    }

    if restore.Spec.DatabaseName == "" {
        return fmt.Errorf("databaseName is required")
    }
    return nil
}
```

**Step 3 — Run tests**

```bash
go test ./internal/controller/... -run "validateRestore|pitr.*valid" -v
```

Expected: PASS.

**Step 4 — Commit**

```bash
git add internal/controller/neo4jrestore_controller.go internal/controller/neo4jrestore_controller_test.go
git commit -m "fix(restore): handle pitr/s3/gcs/azure source types in validateRestore"
```

---

## Task 10 — Restore controller: fix PITR (replace non-existent commands)

**Files:**
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/neo4jrestore_controller_test.go`

**Context:** `buildPITRRestoreCommand` uses three commands that do not exist: `neo4j-admin inspect-backup`, `neo4j-admin validate-transaction-logs`, and `neo4j-admin apply-transaction-logs`. PITR is handled in Neo4j by the `--restore-until=<timestamp-or-txid>` flag on `neo4j-admin database restore`. The correct inspection command is `neo4j-admin database backup --inspect-path=<path>`.

**Step 1 — Write failing test**

```go
It("PITR restore command should use --restore-until not non-existent commands", func() {
    restore := buildTestRestore("pitr-restore", testNamespace)
    restore.Spec.Source.Type = "pitr"
    pitTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
    restore.Spec.Source.PointInTime = &metav1.Time{Time: pitTime}
    restore.Spec.Source.PITR = &neo4jv1alpha1.PITRConfig{
        BaseBackup: &neo4jv1alpha1.BaseBackupSource{
            Type:       "storage",
            BackupPath: "/backups/neo4j-full.backup",
        },
    }

    cmd, err := reconciler.buildPITRRestoreCommand(ctx, restore)
    Expect(err).NotTo(HaveOccurred())
    Expect(cmd).To(ContainSubstring("neo4j-admin database restore"))
    Expect(cmd).To(ContainSubstring("--restore-until="))
    Expect(cmd).NotTo(ContainSubstring("validate-transaction-logs"))
    Expect(cmd).NotTo(ContainSubstring("apply-transaction-logs"))
    Expect(cmd).NotTo(ContainSubstring("neo4j-admin inspect-backup"))
})
```

**Step 2 — Rewrite `buildPITRRestoreCommand`**

```go
func (r *Neo4jRestoreReconciler) buildPITRRestoreCommand(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) (string, error) {
    pitrConfig := restore.Spec.Source.PITR
    if pitrConfig == nil {
        return "", fmt.Errorf("PITR configuration is required for PITR restore")
    }

    clusterKey := types.NamespacedName{Name: restore.Spec.TargetCluster, Namespace: restore.Namespace}
    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
    if err := r.Get(ctx, clusterKey, cluster); err != nil {
        return "", fmt.Errorf("failed to get cluster: %w", err)
    }

    imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
    version, err := neo4j.GetImageVersion(imageTag)
    if err != nil {
        version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
    }

    // Determine base backup path
    var backupPath string
    if pitrConfig.BaseBackup != nil {
        switch pitrConfig.BaseBackup.Type {
        case "backup":
            backupPath = fmt.Sprintf("/backup/%s", pitrConfig.BaseBackup.BackupRef)
        case "storage":
            backupPath = pitrConfig.BaseBackup.BackupPath
        default:
            return "", fmt.Errorf("invalid base backup type: %s", pitrConfig.BaseBackup.Type)
        }
    }

    // Build restore command with --from-path pointing to backup artifact(s)
    cmd := neo4j.GetRestoreCommand(version, restore.Spec.DatabaseName, backupPath)

    if restore.Spec.Force {
        cmd += " --overwrite-destination=true"
    }

    // --restore-until is the PITR mechanism in Neo4j (NOT a separate apply-logs command)
    if restore.Spec.Source.PointInTime != nil {
        // Format: "2021-09-11 10:15:30" (UTC) or a transaction ID
        cmd += fmt.Sprintf(` --restore-until="%s"`,
            restore.Spec.Source.PointInTime.UTC().Format("2006-01-02 15:04:05"))
    }

    // Optional: inspect artifacts before restoring (using the correct command)
    if pitrConfig.ValidateLogIntegrity && backupPath != "" {
        inspectCmd := fmt.Sprintf("neo4j-admin database backup --inspect-path=%s", backupPath)
        cmd = inspectCmd + " && " + cmd
    }

    return cmd, nil
}
```

**Step 3 — Run test**

```bash
go test ./internal/controller/... -run "PITR restore command" -v
```

Expected: PASS.

**Step 4 — Commit**

```bash
git add internal/controller/neo4jrestore_controller.go internal/controller/neo4jrestore_controller_test.go
git commit -m "fix(restore): replace non-existent PITR commands with --restore-until flag"
```

---

## Task 11 — Restore controller: add `CREATE DATABASE` step + fix restore volumes

**Files:**
- Modify: `internal/controller/neo4jrestore_controller.go`
- Modify: `internal/controller/neo4jrestore_controller_test.go`

**Context:** After a successful restore Job, Neo4j requires `CREATE DATABASE <dbname>` to be run against the `system` database before the restored database is accessible. This step is completely missing. Also, the restore Job currently mounts an EmptyDir for both `backup-storage` and `neo4j-data` — the data is written nowhere persistent.

**Note on offline restore data volume:** For `spec.stopCluster: true` (offline restore), the restore needs to write directly to the Neo4j server's data PVC. The server's data PVC for pod `{cluster}-server-0` is named `data-{cluster}-server-0`. This plan implements the `CREATE DATABASE` step and uses the server's data PVC for offline restores. Online restore (stop=false) restores to a separate PVC and then uses seeding.

**Step 1 — Write failing test for CREATE DATABASE**

```go
It("should run CREATE DATABASE after successful restore Job", func() {
    // Setup: cluster is ready, restore Job succeeded
    // Expect: CREATE DATABASE called on the Bolt client
    mockNeo4jClient := &MockNeo4jClient{}
    reconciler.Neo4jClientFactory = func(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, ...) (*neo4j.Client, error) {
        return mockNeo4jClient, nil
    }

    result, err := reconciler.handleRestoreSuccess(ctx, restore, cluster, job)
    Expect(err).NotTo(HaveOccurred())
    Expect(mockNeo4jClient.CreateDatabaseCalled).To(BeTrue())
    Expect(mockNeo4jClient.LastCreatedDatabase).To(Equal("mydb"))
    _ = result
})
```

**Step 2 — Add `CREATE DATABASE` call in `handleRestoreSuccess`**

In the `handleRestoreSuccess` function, after the cluster is restarted (or immediately for non-stop restores), add:

```go
// For new databases (not overwriting existing), create the database in Neo4j.
// For replaceExisting / force restores against a stopped cluster, the database
// already exists and we just need to start it.
if !restore.Spec.Force && (restore.Spec.Options == nil || !restore.Spec.Options.ReplaceExisting) {
    if err := r.createRestoredDatabase(ctx, restore, cluster); err != nil {
        logger.Error(err, "Failed to create restored database")
        r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to create database after restore: %v", err))
        return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
    }
}
```

Add `createRestoredDatabase`:

```go
func (r *Neo4jRestoreReconciler) createRestoredDatabase(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
    neo4jClient, err := r.createNeo4jClient(ctx, cluster)
    if err != nil {
        return fmt.Errorf("failed to create Neo4j client: %w", err)
    }
    defer func() { _ = neo4jClient.Close() }()

    exists, err := neo4jClient.DatabaseExists(ctx, restore.Spec.DatabaseName)
    if err != nil {
        return fmt.Errorf("failed to check database existence: %w", err)
    }
    if exists {
        // Database already exists (e.g. replaced in-place), just start it.
        return neo4jClient.StartDatabase(ctx, restore.Spec.DatabaseName)
    }
    // New database restore: create it so Neo4j loads the restored store.
    return neo4jClient.CreateDatabase(ctx, restore.Spec.DatabaseName, false)
}
```

Also add `StartDatabase` and `CreateDatabase` methods to `internal/neo4j/client.go` if they don't already exist (check first with grep).

**Step 3 — Fix `buildRestoreVolumes` for PVC backup source**

When the restore source is a `Neo4jBackup` resource (type="backup"), we need to resolve the actual backup PVC from the backup spec. Replace the EmptyDir stub:

```go
case "backup":
    // Resolve backup's storage to get the actual PVC name
    backup := &neo4jv1alpha1.Neo4jBackup{}
    // (in buildRestoreVolumes, pass backup object or PVC name as parameter)
    // If backup uses PVC storage, mount that PVC:
    if backup.Spec.Storage.Type == "pvc" && backup.Spec.Storage.PVC != nil && backup.Spec.Storage.PVC.Name != "" {
        volumes = append(volumes, corev1.Volume{
            Name: "backup-storage",
            VolumeSource: corev1.VolumeSource{
                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                    ClaimName: backup.Spec.Storage.PVC.Name,
                    ReadOnly:  true,
                },
            },
        })
    }
```

**Step 4 — Run tests**

```bash
go test ./internal/controller/... -run "Neo4jRestore" -v
```

Expected: PASS.

**Step 5 — Commit**

```bash
git add internal/controller/neo4jrestore_controller.go internal/controller/neo4jrestore_controller_test.go internal/neo4j/client.go
git commit -m "fix(restore): add CREATE DATABASE step after restore, fix PVC volume mounting"
```

---

## Task 12 — Restore controller: add standalone support

**Files:**
- Modify: `internal/controller/neo4jrestore_controller.go`

**Context:** `getTargetCluster` only fetches `Neo4jEnterpriseCluster`. Standalone deployments are not supported. The same dual-lookup pattern from the database controller should be applied.

**Step 1 — Fix `getTargetCluster` in restore controller**

Mirror the pattern from Task 5. Try `Neo4jEnterpriseCluster` first, then fall back to `Neo4jEnterpriseStandalone`:

```go
func (r *Neo4jRestoreReconciler) getTargetCluster(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
    key := types.NamespacedName{Name: restore.Spec.TargetCluster, Namespace: restore.Namespace}

    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
    if err := r.Get(ctx, key, cluster); err == nil {
        return cluster, nil
    }

    standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
    if err := r.Get(ctx, key, standalone); err != nil {
        return nil, fmt.Errorf("target %q not found as cluster or standalone: %w", restore.Spec.TargetCluster, err)
    }
    return standaloneToCluster(standalone), nil
}
```

Move the `standaloneToCluster` helper to a shared location (e.g. `internal/controller/util.go`) so both backup and restore controllers can use it.

**Step 2 — Run tests and commit**

```bash
go test ./internal/controller/... -run "Neo4jRestore" -v
git add internal/controller/neo4jrestore_controller.go internal/controller/util.go
git commit -m "fix(restore): add Neo4jEnterpriseStandalone support to restore controller"
```

---

## Task 13 — Retention policy: implement actual artifact cleanup

**Files:**
- Modify: `internal/controller/neo4jbackup_controller.go`
- Modify: `internal/controller/neo4jbackup_controller_test.go`

**Context:** `cleanupBackupArtifacts` creates a Job that only echoes messages and does nothing. For PVC storage, retention can be implemented by listing backup directories (using `neo4j-admin database backup --inspect-path`) and deleting old ones. For cloud storage, the native Neo4j backup chain inspection can be used similarly.

**Step 1 — Write failing test**

```go
It("cleanup job for PVC storage should run neo4j-admin inspect then delete old artifacts", func() {
    backup := buildScheduledTestBackup(testNamespace)
    backup.Spec.Retention = &neo4jv1alpha1.RetentionPolicy{MaxCount: 3}

    err := reconciler.cleanupBackupArtifacts(ctx, backup)
    Expect(err).NotTo(HaveOccurred())

    jobList := &batchv1.JobList{}
    Expect(k8sClient.List(ctx, jobList, client.InNamespace(testNamespace), client.MatchingLabels{
        "app.kubernetes.io/component": "cleanup",
    })).To(Succeed())
    Expect(jobList.Items).To(HaveLen(1))

    cleanupScript := jobList.Items[0].Spec.Template.Spec.Containers[0].Args[1]
    Expect(cleanupScript).To(ContainSubstring("neo4j-admin database backup --inspect-path"))
    Expect(cleanupScript).NotTo(ContainSubstring(`echo "Implementation would"`))
})
```

**Step 2 — Implement retention cleanup for PVC**

The cleanup Job should:
1. Run `neo4j-admin database backup --inspect-path=/backup` to list artifacts with metadata
2. Parse the output (artifact files, timestamps)
3. Apply `maxCount` and `maxAge` retention rules
4. Delete artifacts that violate retention

Since parsing neo4j-admin output in a shell script is complex, use a pragmatic approach: sort artifacts by modification time and delete the oldest beyond `maxCount`, and delete those older than `maxAge`:

```go
func (r *Neo4jBackupReconciler) cleanupBackupArtifacts(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) error {
    if backup.Spec.Retention == nil {
        return nil
    }
    if backup.Spec.Storage.Type != "pvc" {
        // Cloud retention is handled natively by the cloud provider lifecycle rules.
        // Log a warning that manual lifecycle policies should be set on the bucket.
        log.FromContext(ctx).Info("Cloud storage retention policy should be configured via bucket lifecycle rules", "backup", backup.Name)
        return nil
    }

    image := "alpine:latest"
    backupDir := "/backup"

    // Build retention script
    script := buildRetentionScript(backup.Spec.Retention, backupDir)

    cleanupJob := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s-cleanup-%d", backup.Name, time.Now().Unix()),
            Namespace: backup.Namespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":      "neo4j-backup",
                "app.kubernetes.io/instance":  backup.Name,
                "app.kubernetes.io/component": "cleanup",
            },
        },
        Spec: batchv1.JobSpec{
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyNever,
                    Containers: []corev1.Container{
                        {
                            Name:         "backup-cleanup",
                            Image:        image,
                            Command:      []string{"/bin/sh"},
                            Args:         []string{"-c", script},
                            VolumeMounts: r.buildVolumeMounts(backup),
                        },
                    },
                    Volumes: r.buildVolumes(backup),
                },
            },
        },
    }

    return r.Create(ctx, cleanupJob)
}

func buildRetentionScript(policy *neo4jv1alpha1.RetentionPolicy, backupDir string) string {
    script := fmt.Sprintf(`#!/bin/sh
set -e
BACKUP_DIR="%s"
echo "Starting backup retention enforcement in $BACKUP_DIR"

# List backup artifacts sorted by modification time (oldest first)
# Backup artifacts are .backup files created by neo4j-admin
FILES=$(find "$BACKUP_DIR" -name "*.backup" -type f | sort -t_ -k1)
FILE_COUNT=$(echo "$FILES" | grep -c . 2>/dev/null || echo 0)
echo "Found $FILE_COUNT backup artifacts"
`, backupDir)

    if policy.MaxCount > 0 {
        script += fmt.Sprintf(`
# Max count: keep only the %d most recent backups
MAX_COUNT=%d
if [ "$FILE_COUNT" -gt "$MAX_COUNT" ]; then
    TO_DELETE=$((FILE_COUNT - MAX_COUNT))
    echo "Deleting $TO_DELETE old backups (max count: $MAX_COUNT)"
    echo "$FILES" | head -n "$TO_DELETE" | xargs -r rm -f
    echo "Deleted $TO_DELETE old backup artifacts"
fi
`, policy.MaxCount, policy.MaxCount)
    }

    if policy.MaxAge != "" {
        script += fmt.Sprintf(`
# Max age: delete backups older than %s
# Convert age string to find -mtime argument (days)
MAX_AGE_ARG=$(echo "%s" | sed 's/d$//' | sed 's/h$//' )
find "$BACKUP_DIR" -name "*.backup" -type f -mtime +$MAX_AGE_ARG -exec rm -f {} \;
echo "Deleted backup artifacts older than %s"
`, policy.MaxAge, policy.MaxAge, policy.MaxAge)
    }

    script += `echo "Backup retention enforcement complete"`
    return script
}
```

**Step 3 — Run tests**

```bash
go test ./internal/controller/... -run "cleanup|retention|Retention" -v
```

Expected: PASS.

**Step 4 — Commit**

```bash
git add internal/controller/neo4jbackup_controller.go internal/controller/neo4jbackup_controller_test.go
git commit -m "fix(backup): implement actual retention policy cleanup for PVC storage"
```

---

## Task 14 — Backup stats: populate from Job completion time

**Files:**
- Modify: `internal/controller/neo4jbackup_controller.go`

**Context:** `updateBackupStats` fills `Size`, `Throughput`, and `FileCount` as `"unknown"`. Duration is the only meaningful value. This task improves the duration calculation and documents the limitation on the other fields (extracting them from neo4j-admin logs requires log parsing which is a future enhancement).

**Step 1 — Update `updateBackupStats` to remove misleading "unknown" strings**

```go
func (r *Neo4jBackupReconciler) updateBackupStats(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, job *batchv1.Job) {
    stats := &neo4jv1alpha1.BackupStats{}

    if job.Status.CompletionTime != nil && job.Status.StartTime != nil {
        duration := job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
        stats.Duration = duration.Round(time.Second).String()
    }
    // Size and throughput require parsing neo4j-admin stdout from Job pod logs.
    // This is a known limitation — set to empty string (not "unknown") to avoid confusing users.

    update := func() error {
        latest := &neo4jv1alpha1.Neo4jBackup{}
        if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
            return err
        }
        latest.Status.Stats = stats
        return r.Status().Update(ctx, latest)
    }
    if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
        log.FromContext(ctx).Error(err, "Failed to update backup stats")
    }
}
```

**Step 2 — Add a history entry per backup run**

In `handleExistingBackupJob`, when job succeeds, append to `Status.History`:

```go
run := neo4jv1alpha1.BackupRun{
    StartTime:  *job.Status.StartTime,
    Status:     "Completed",
}
if job.Status.CompletionTime != nil {
    run.CompletionTime = job.Status.CompletionTime
    run.Stats = &neo4jv1alpha1.BackupStats{
        Duration: job.Status.CompletionTime.Sub(job.Status.StartTime.Time).Round(time.Second).String(),
    }
}
// Keep last 10 runs in history
latest.Status.History = append([]neo4jv1alpha1.BackupRun{run}, latest.Status.History...)
if len(latest.Status.History) > 10 {
    latest.Status.History = latest.Status.History[:10]
}
```

**Step 3 — Run unit tests**

```bash
make test-unit
```

Expected: PASS.

**Step 4 — Commit**

```bash
git add internal/controller/neo4jbackup_controller.go
git commit -m "fix(backup): remove 'unknown' placeholder stats, track run history"
```

---

## Task 15 — Final: run full test suite and clean up lint

**Step 1 — Run unit tests**

```bash
make test-unit
```

Expected: all PASS with 0 failures.

**Step 2 — Run linter**

```bash
make lint-lenient
```

Fix any introduced lint errors.

**Step 3 — Verify build**

```bash
make build
```

**Step 4 — Run manifests regeneration to ensure CRDs are current**

```bash
make manifests generate
git diff --name-only config/ charts/ bundle/
```

Commit any CRD drift.

**Step 5 — Final commit**

```bash
git add -A
git commit -m "chore: regenerate CRDs, fix lint after backup/restore overhaul"
```

---

## Out of Scope (Architectural Decisions Needed Separately)

The following issues are acknowledged but deferred — they require design decisions beyond bug-fixing:

1. **Designated seeder pattern for cluster restores** — Restoring to a specific server pod's data directory and triggering cluster-wide database creation requires deciding which server to seed, PVC access patterns, and cluster quorum considerations.

2. **`BackupOptions.Compress` bool vs `*bool`** — A breaking API change. Can be done in a separate PR with migration documentation.

3. **Backup stats from neo4j-admin log parsing** — Requires a log-collection init container or sidecar to parse `neo4j-admin` stdout. Deferred to observability work.

4. **`--include-metadata` user-filter (2025.10+)** — New feature addition rather than bug fix; requires new API fields.

5. **Cloud retention via bucket lifecycle** — Native cloud lifecycle policies (S3 Lifecycle Rules, GCS Object Lifecycle, Azure Blob Lifecycle) are more robust than operator-managed cleanup. A separate docs/user-guide addition is recommended.
