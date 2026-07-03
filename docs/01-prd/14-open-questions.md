# Open questions

Decisions that block or shape GA commitment. Resolve owners before freezing V1 scope.

| # | Question | Owner | Blocks |
|---|----------|-------|--------|
| 1 | Product Engineering sponsorship and long-term ownership? | Product + PS | GA, support model |
| 2 | Official support tier and SLA for operator-managed deployments? | Product + Support | Enterprise adoption |
| 3 | Supported Helm → operator migration path for installed base? | PS + Product | Migration risk |
| 4 | Operator value proposition vs Helm + GitOps — what is V1-only? | Product + PS | Differentiation risk |
| 5 | Ratify [BDR-012](../02-technical-design/decision-records/business/012-identity-management.md) identity model (`User` / `Role` / `Grant`) and V2 priority vs backup? | Product + Security | Roadmap sequencing |
| 6 | Optional Web UI — product commitment or GitOps-only forever? | Product | Adoption for non-Git personas |
| 7 | cert-manager as recommended TLS path post-V1? | PS + Product | V1.1 HTTPS / ingress |

---

## Related technical decisions (BDR)

| BDR | Status | Question |
|-----|--------|----------|
| [BDR-003](../02-technical-design/decision-records/business/operator/003-operator-install-scope.md) | proposed | Ratify single-namespace V1 |
| [BDR-012](../02-technical-design/decision-records/business/identity-user-roles/012-identity-management.md) | proposed | Ratify identity CRDs (`Neo4jUser` / `Neo4jRole` / `Neo4jGrant`) — post-V1 |

Track resolution in [`../02-technical-design/decision-records/readme.md`](../02-technical-design/decision-records/readme.md).
