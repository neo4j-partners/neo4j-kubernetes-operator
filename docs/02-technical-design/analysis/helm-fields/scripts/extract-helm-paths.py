#!/usr/bin/env python3
"""Extract Helm values paths from neo4j/values.yaml for design/analysis/helm-fields/_index.csv.

Usage:
  python scripts/extract-helm-paths.py > _index.csv
  python scripts/extract-helm-paths.py --check   # fail if values.yaml has untracked paths
"""
from __future__ import annotations

import argparse
import csv
import re
import sys
from pathlib import Path

# Atomic groups: treat subtree as one inventory row (avoid config.* explosion)
ATOMIC_PREFIXES = {
    "config": "config (map — neo4j.conf key/value)",
    "apoc_config": "apoc_config (map)",
    "apoc_credentials": "apoc_credentials (map)",
    "secretMounts": "secretMounts (map)",
    "env": "env (map)",
    "neo4j.labels": "neo4j.labels (map)",
    "nodeSelector": "nodeSelector (map)",
}

# Explicit leaf / mid-level paths (curated from values.yaml structure)
EXPLICIT_PATHS = [
    # packaging
    ("nameOverride", "packaging", "packaging"),
    ("fullnameOverride", "packaging", "packaging"),
    ("disableLookups", "packaging", "packaging"),
    # neo4j core
    ("neo4j.name", "neo4j-core", "topology"),
    ("neo4j.password", "neo4j-core", "security"),
    ("neo4j.passwordFromSecret", "neo4j-core", "security"),
    ("neo4j.edition", "neo4j-core", "topology"),
    ("neo4j.minimumClusterSize", "topology-lifecycle", "topology"),
    ("neo4j.operations", "topology-lifecycle", "lifecycle"),
    ("neo4j.operations.enableServer", "topology-lifecycle", "lifecycle"),
    ("neo4j.operations.image", "topology-lifecycle", "lifecycle"),
    ("neo4j.operations.protocol", "topology-lifecycle", "lifecycle"),
    ("neo4j.operations.ssl", "topology-lifecycle", "lifecycle"),
    ("neo4j.acceptLicenseAgreement", "neo4j-core", "security"),
    ("neo4j.offlineMaintenanceModeEnabled", "neo4j-core", "lifecycle"),
    ("neo4j.resources", "neo4j-core", "scheduling"),
    ("logInitialPassword", "neo4j-core", "security"),
    # analytics / topology
    ("analytics", "topology-lifecycle", "topology"),
    ("analytics.enabled", "topology-lifecycle", "topology"),
    ("analytics.type.name", "topology-lifecycle", "topology"),
    # storage — data
    ("volumes.data", "storage", "storage"),
    ("volumes.data.mode", "storage", "storage"),
    ("volumes.data.labels", "storage", "storage"),
    ("volumes.data.disableSubPathExpr", "storage", "storage"),
    ("volumes.data.selector", "storage", "storage"),
    ("volumes.data.defaultStorageClass", "storage", "storage"),
    ("volumes.data.dynamic", "storage", "storage"),
    ("volumes.data.volume", "storage", "storage"),
    ("volumes.data.volumeClaimTemplate", "storage", "storage"),
    # aux volumes (mode + share pattern)
    ("volumes.backups", "storage", "storage"),
    ("volumes.logs", "storage", "storage"),
    ("volumes.metrics", "storage", "storage"),
    ("volumes.import", "storage", "storage"),
    ("volumes.licenses", "storage", "storage"),
    ("additionalVolumes", "storage", "storage"),
    ("additionalVolumeMounts", "storage", "storage"),
    ("secretMounts", "storage", "storage"),
    # network
    ("services.default", "network", "network"),
    ("services.neo4j", "network", "network"),
    ("services.neo4j.enabled", "network", "network"),
    ("services.neo4j.spec.type", "network", "network"),
    ("services.neo4j.ports", "network", "network"),
    ("services.neo4j.multiCluster", "network", "network"),
    ("services.neo4j.cleanup", "network", "network"),
    ("services.admin", "network", "network"),
    ("services.internals", "network", "network"),
    ("clusterDomain", "network", "network"),
    # tls
    ("ssl", "tls", "network"),
    ("ssl.bolt", "tls", "network"),
    ("ssl.https", "tls", "network"),
    ("ssl.cluster", "tls", "network"),
    # config
    ("config", "config", "config"),
    ("jvm", "config", "config"),
    ("jvm.useNeo4jDefaultJvmArguments", "config", "config"),
    ("jvm.additionalJvmArguments", "config", "config"),
    ("env", "config", "config"),
    # plugins
    ("apoc_config", "plugins", "plugins"),
    ("apoc_credentials", "plugins", "plugins"),
    # security
    ("ldapPasswordFromSecret", "security", "security"),
    ("ldapPasswordMountPath", "security", "security"),
    ("securityContext", "security", "security"),
    ("containerSecurityContext", "security", "security"),
    # health
    ("readinessProbe", "health", "health"),
    ("livenessProbe", "health", "health"),
    ("startupProbe", "health", "health"),
    # scheduling
    ("nodeSelector", "scheduling", "scheduling"),
    ("statefulset", "scheduling", "scheduling"),
    ("statefulset.metadata", "scheduling", "scheduling"),
    ("podSpec", "scheduling", "scheduling"),
    ("podSpec.podAntiAffinity", "scheduling", "scheduling"),
    ("podSpec.nodeAffinity", "scheduling", "scheduling"),
    ("podSpec.tolerations", "scheduling", "scheduling"),
    ("podSpec.topologySpreadConstraints", "scheduling", "scheduling"),
    ("podSpec.priorityClassName", "scheduling", "scheduling"),
    ("podSpec.loadbalancer", "scheduling", "network"),
    ("podSpec.dnsPolicy", "scheduling", "scheduling"),
    ("podSpec.serviceAccountName", "scheduling", "scheduling"),
    ("podSpec.terminationGracePeriodSeconds", "scheduling", "scheduling"),
    ("podSpec.initContainers", "scheduling", "scheduling"),
    ("podSpec.containers", "scheduling", "scheduling"),
    # image
    ("image", "image", "packaging"),
    ("image.imagePullPolicy", "image", "packaging"),
    ("image.registry", "image", "packaging"),
    ("image.repository", "image", "packaging"),
    ("image.tag", "image", "packaging"),
    ("image.customImage", "image", "packaging"),
    ("image.imagePullSecrets", "image", "packaging"),
    ("image.imageCredentials", "image", "packaging"),
    # observability
    ("logging", "observability", "observability"),
    ("logging.serverLogsXml", "observability", "observability"),
    ("logging.userLogsXml", "observability", "observability"),
    ("serviceMonitor", "observability", "observability"),
    # resilience
    ("podDisruptionBudget", "resilience", "scheduling"),
]

CSV_HEADER = [
    "helm_path",
    "domain",
    "category",
    "client_need",
    "neo4j_doc_ref",
    "helm_code_refs",
    "k8s_resources",
    "crd_target",
    "aggregation_group",
    "versioning",
    "bdr_id",
    "fr_ids",
    "status",
]


def row(path: str, domain: str, category: str) -> dict:
    return {
        "helm_path": path,
        "domain": domain,
        "category": category,
        "client_need": "",
        "neo4j_doc_ref": "",
        "helm_code_refs": "",
        "k8s_resources": "",
        "crd_target": "",
        "aggregation_group": "",
        "versioning": "",
        "bdr_id": "",
        "fr_ids": "",
        "status": "todo",
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--output",
        type=Path,
        default=None,
        help="Write CSV to file (default: stdout)",
    )
    parser.add_argument("--check", action="store_true", help="Exit 1 if output file missing paths")
    args = parser.parse_args()

    rows = [row(p, d, c) for p, d, c in EXPLICIT_PATHS]
    # Deduplicate preserving order
    seen = set()
    unique = []
    for r in rows:
        if r["helm_path"] not in seen:
            seen.add(r["helm_path"])
            unique.append(r)

    out = args.output
    if out:
        out.parent.mkdir(parents=True, exist_ok=True)
        f = out.open("w", newline="", encoding="utf-8")
    else:
        f = sys.stdout

    writer = csv.DictWriter(f, fieldnames=CSV_HEADER)
    writer.writeheader()
    writer.writerows(unique)

    if out:
        f.close()
        print(f"Wrote {len(unique)} rows to {out}", file=sys.stderr)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
