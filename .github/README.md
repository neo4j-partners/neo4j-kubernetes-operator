# GitHub Actions

## Workflow: `ci.yml`

| Job | Description |
|-----|-------------|
| **unit** | `make test` + `make audit` |
| **e2e-local-kind** | kind cluster + `tests/bin/run-e2e.sh` |
| **e2e-azure-aks** | Azure login, ensure AKS, push image, e2e suite |

## Azure setup

1. Create a service principal with Contributor on the subscription.
2. Add repository secrets:
   - `AZURE_CREDENTIALS` — full JSON from `--sdk-auth`
   - `AZURE_SUBSCRIPTION_ID` — subscription GUID
3. (Optional) Set repository variables for resource names — see [tests/README.md](../tests/README.md).

The Azure job reuses an existing AKS cluster when present; otherwise it creates RG + ACR + AKS.
