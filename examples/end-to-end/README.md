# End-to-End Examples

Complete, multi-resource scenarios that combine a cluster/standalone with
databases, plugins, backups, users, networking, etc. — to show how the CRDs fit
together. Each file is a self-contained walkthrough (read the comments inside).

| Example | Description |
|---|---|
| [`complete-deployment.yaml`](complete-deployment.yaml) | Full deployment: cluster + database + plugin + backup + monitoring |
| [`development-workflow.yaml`](development-workflow.yaml) | Standalone-based dev/test inner-loop workflow |
| [`disaster-recovery.yaml`](disaster-recovery.yaml) | Backup + restore / DR patterns |
| [`multi-tenancy.yaml`](multi-tenancy.yaml) | Multiple isolated databases with quotas, network policy, and per-tenant config |

> These bundle several resources and reference Secrets/namespaces that you must
> create first — read the comments in each file before applying.
