# Test: backup + restore on Azure Blob with Workload Identity (AKS)

End-to-end steps to verify the cloud-identity path this operator wires (ADR-016): a `Neo4jBackup`
that **writes** to `azb://…` and a `Neo4jRestore` that **reads** it back — both authenticated by
**Azure Workload Identity**, no static keys. It exercises exactly the plumbing the operator adds: a
dedicated `<name>-backup` ServiceAccount for the backup Job and the operand ServiceAccount for the
server pods, each stamped with the `azure.workload.identity/client-id` annotation, plus the
`azure.workload.identity/use` pod label that makes the AKS webhook inject a federated token.

## How it works (the short version)

Workload Identity means **no keys in the cluster**. It's a passport + visa:

- **Passport** — AKS gives each pod a short-lived signed token saying *"I am ServiceAccount
  `<namespace>:<name>`"* — but only if the pod has the `azure.workload.identity/use` label (the
  operator adds it). The name in the token is the **subject**.
- **Visa (federated credential)** — you tell Azure once: "trust tokens whose subject is exactly
  `system:serviceaccount:<ns>:<sa>` and let them become this managed identity." **The subject must
  match to the character.**
- **Keycard (role)** — the managed identity has `Storage Blob Data Contributor` on the account.

**Two pods touch the blob, so you federate two subjects** (this is the #1 mistake):

| Pod | ServiceAccount (subject) | For |
|-----|--------------------------|-----|
| Backup Job | `${NS}:${NEO4J}-backup` | backup **writes** (steps 4, 6) |
| Neo4j server | `${NS}:${NEO4J}` | restore **reads** (steps 8–9) |

> Two gotchas that produce "provided uri does not point to a valid location" on restore: a **subject
> typo** (the server pod's token then matches no visa), and running an **operator too old** to stamp
> the server-pod label (the pod then gets no token). Step 9 verifies both.

## Prerequisites

- An AKS cluster and `az` + `kubectl` logged in to it.
- The operator installed on the cluster (see `../../docs/user-guide/02-operator-installation/03-install.md`).
- Neo4j **Enterprise**; a database with the **backup listener** enabled (the example below does this).
- Azure Workload Identity requires the mutating webhook, which comes with `--enable-workload-identity`.

## 0. Shell variables

```bash
export RG=my-rg
export CLUSTER=my-aks
export LOCATION=eastus
export NS=default                       # namespace you deploy Neo4j into
export NEO4J=cloud-identity             # Neo4j CR name → backup SA is "cloud-identity-backup"
export STORAGE=neo4jbackups$RANDOM      # storage account (3-24 lowercase alnum, globally unique)
export CONTAINER=backups
export UAMI=neo4j-backup-identity       # user-assigned managed identity
```

## 1. Enable OIDC issuer + Workload Identity on the cluster (skip if already on)

```bash
az aks update -g "$RG" -n "$CLUSTER" --enable-oidc-issuer --enable-workload-identity
export OIDC_ISSUER=$(az aks show -g "$RG" -n "$CLUSTER" --query oidcIssuerProfile.issuerUrl -o tsv)
echo "$OIDC_ISSUER"
```

## 2. Storage account + container (the backup destination)

```bash
az storage account create -g "$RG" -n "$STORAGE" -l "$LOCATION" --sku Standard_LRS
az storage container create --account-name "$STORAGE" -n "$CONTAINER" --auth-mode login
export STORAGE_ID=$(az storage account show -g "$RG" -n "$STORAGE" --query id -o tsv)
```

## 3. Managed identity + grant it blob write on the storage

```bash
az identity create -g "$RG" -n "$UAMI" -l "$LOCATION"
export UAMI_CLIENT_ID=$(az identity show -g "$RG" -n "$UAMI" --query clientId -o tsv)
export UAMI_PRINCIPAL_ID=$(az identity show -g "$RG" -n "$UAMI" --query principalId -o tsv)

az role assignment create \
  --assignee-object-id "$UAMI_PRINCIPAL_ID" \
  --assignee-principal-type ServicePrincipal \
  --role "Storage Blob Data Contributor" \
  --scope "$STORAGE_ID"
```

## 4. Federate the identity to the **backup** ServiceAccount

The subject is the SA the backup pod runs as — `<NEO4J>-backup` — **not** the operand SA. It must
match exactly (case-sensitive), or the token exchange fails silently at backup time.

```bash
az identity federated-credential create \
  --name neo4j-backup-fic \
  --identity-name "$UAMI" \
  --resource-group "$RG" \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:${NS}:${NEO4J}-backup" \
  --audience api://AzureADTokenExchange
```

## 5. Deploy Neo4j with the Azure cloud identity

Apply this (a variant of `25-cloud-identity.yaml` with the Azure provider and your client id):

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: ${NEO4J}
spec:
  edition: enterprise
  version: "2026.05.0"
  license: { accept: "yes" }
  topology: { mode: Standalone }
  features: { backup: { enabled: true } }
  connectivity: { listeners: { backup: 6362 } }
  security:
    cloudIdentity:
      workloadIdentity:
        provider: azure
        annotations:
          azure.workload.identity/client-id: ${UAMI_CLIENT_ID}
  storage: { volumes: { data: { mode: Dynamic, dynamic: { size: 10Gi } } } }
  auth: { generatePassword: true }
YAML

kubectl -n "$NS" wait --for=condition=Ready neo4j/${NEO4J} --timeout=600s
```

Confirm the operator wired the identity onto the backup SA:

```bash
kubectl -n "$NS" get sa ${NEO4J}-backup -o jsonpath='{.metadata.annotations}'; echo
# expect: {"azure.workload.identity/client-id":"<your UAMI client id>"}
```

## 6. Run the backup to Azure

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: bk-azure
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  type: Full
  destination:
    type: azure
    url: azb://${STORAGE}/${CONTAINER}/neo4j/   # trailing '/' — neo4j-admin needs a directory
YAML

kubectl -n "$NS" get neo4jbackup bk-azure -w   # wait for PHASE=Succeeded (Ctrl-C to stop)
```

## 7. Verify

Operator plumbing — the backup pod runs as the backup SA, carries the Azure label, and the AKS
webhook injected the federated-token env:

```bash
POD=$(kubectl -n "$NS" get pod -l job-name=bk-azure-backup -o name | head -1)
kubectl -n "$NS" get "$POD" -o jsonpath='{.spec.serviceAccountName}{"\n"}'          # cloud-identity-backup
kubectl -n "$NS" get "$POD" -o jsonpath='{.metadata.labels.azure\.workload\.identity/use}{"\n"}'  # true
kubectl -n "$NS" get "$POD" -o jsonpath='{range .spec.containers[0].env[*]}{.name}{"\n"}{end}' | grep AZURE
#   expect AZURE_CLIENT_ID, AZURE_TENANT_ID, AZURE_FEDERATED_TOKEN_FILE, AZURE_AUTHORITY_HOST
```

The artifact actually landed in the bucket, and the backup logs are clean:

```bash
kubectl -n "$NS" logs job/bk-azure-backup | tail -20      # "Backup ... completed"
az storage blob list --account-name "$STORAGE" -c "$CONTAINER" --prefix neo4j --auth-mode login -o table
kubectl -n "$NS" get neo4jbackup bk-azure -o jsonpath='{.status.phase} {.status.artifacts}{"\n"}'
```

If PHASE=Failed, `kubectl -n "$NS" describe neo4jbackup bk-azure` shows the neo4j-admin cause; the
usual suspects are a mismatched federated-credential subject (step 4) or a missing role assignment
(step 3).

## 8. Federate the **operand** ServiceAccount (for restore reads)

The restore reads the blob from the **Neo4j server pods**, which run as the operand SA `${NEO4J}` —
a *different* subject from the backup SA. Add its visa (same identity, same bucket keycard):

```bash
az identity federated-credential create \
  --name neo4j-operand-fic \
  --identity-name "$UAMI" \
  --resource-group "$RG" \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:${NS}:${NEO4J}" \
  --audience api://AzureADTokenExchange
```

The `Neo4j` from step 5 already carries `spec.security.cloudIdentity.workloadIdentity`, so the
operator has already stamped the operand SA annotation and the server-pod label — no manifest change.

## 9. Restore from Azure and verify

First confirm the identity is live on the server pod (the check that catches an old operator image):

```bash
kubectl -n "$NS" get pod ${NEO4J}-server-0 -o jsonpath='{.metadata.labels.azure\.workload\.identity/use}{"\n"}'  # true
kubectl -n "$NS" exec ${NEO4J}-server-0 -c neo4j -- env | grep AZURE
#   expect AZURE_CLIENT_ID, AZURE_TENANT_ID, AZURE_FEDERATED_TOKEN_FILE, AZURE_AUTHORITY_HOST
kubectl -n "$NS" get sa ${NEO4J} -o jsonpath='{.metadata.annotations}'; echo   # azure.workload.identity/client-id
```

Then restore the backup you took in step 6 (the operator resolves the `azb://` location from the
`Neo4jBackup` record and seeds via `CloudSeedProvider`):

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata: { name: rst-azure }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  overwrite: true            # replace the existing database with the restored store
  source: { backupRef: bk-azure }
YAML

kubectl -n "$NS" get neo4jrestore rst-azure -o jsonpath='{.status.phase} {.status.conditions}{"\n"}' -w
```

If it fails with `RestoreSeedFailed … does not point to a valid location`, it is almost always the
operand visa: re-check the step-8 subject is **exactly** `system:serviceaccount:${NS}:${NEO4J}` and
that step 9's pod check shows the label + `AZURE_*` env.

## Cleanup

```bash
kubectl -n "$NS" delete neo4jrestore rst-azure --ignore-not-found
kubectl -n "$NS" delete neo4jbackup bk-azure
kubectl -n "$NS" delete neo4j ${NEO4J}
az identity federated-credential delete --name neo4j-backup-fic --identity-name "$UAMI" -g "$RG" --yes
az identity federated-credential delete --name neo4j-operand-fic --identity-name "$UAMI" -g "$RG" --yes
az identity delete -g "$RG" -n "$UAMI"
az storage account delete -g "$RG" -n "$STORAGE" --yes
```

## Static-key alternative (portable, no Workload Identity)

Skip steps 1, 3, 4, and the `security.cloudIdentity` block in step 5 (a static-key *backup* needs
nothing on the Neo4j beyond the backup listener). Instead, reference a Secret from the backup's own
`destination.credentials` — the operator projects its keys as env into the backup pod, so no SA
federation is involved and it can vary per backup:

```bash
kubectl -n "$NS" create secret generic neo4j-cloud-creds \
  --from-literal=AZURE_STORAGE_ACCOUNT="$STORAGE" \
  --from-literal=AZURE_STORAGE_KEY="$(az storage account keys list -g "$RG" -n "$STORAGE" --query [0].value -o tsv)"
```

```bash
cat <<YAML | kubectl apply -n "$NS" -f -
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: bk-azure-key }
spec:
  neo4jRef: { name: ${NEO4J} }
  databases: ["neo4j"]
  type: Full
  destination:
    type: azure
    url: azb://${STORAGE}/${CONTAINER}/neo4j/
    credentials:
      secretName: neo4j-cloud-creds
YAML
```

> The exact Secret keys Neo4j expects for Azure are version-dependent (2025.6.0+ tightened the
> requirements). If the backup fails with a credentials error, check the keys against
> [Neo4j — back up to cloud storage](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/)
> and add what it lists (e.g. `AZURE_STORAGE_ACCOUNT`, `AZURE_STORAGE_KEY`).
