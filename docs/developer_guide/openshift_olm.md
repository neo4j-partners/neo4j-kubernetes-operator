# OpenShift OLM Packaging

This project includes make targets for building an Operator Lifecycle Manager (OLM) bundle and catalog for OpenShift. Use these steps on a workstation with Docker/Podman and operator-sdk/opm available (Makefile will download pinned versions).

## Build the bundle

```bash
# Generate bundle manifests from the current workspace.
# `make bundle` pins the CSV's createdAt: annotation to a stable placeholder
# (2020-01-01T00:00:00Z) so concurrent PRs don't conflict on the timestamp
# and `make check-drift` stays deterministic. Use this target for local
# development, drift checks, and CI.
make bundle VERSION=0.0.1 IMG=ghcr.io/priyolahiri/neo4j-kubernetes-operator:latest

# Build and push bundle image
# (the Makefile default tag is v$(VERSION), e.g. v0.0.1 — keep the `v` prefix)
make bundle-build BUNDLE_IMG=quay.io/your-org/neo4j-operator-bundle:v0.0.1
make bundle-push BUNDLE_IMG=quay.io/your-org/neo4j-operator-bundle:v0.0.1
```

> **Release flow:** when publishing to OperatorHub, run `make bundle-release`
> instead of `make bundle`. That target stamps the real `createdAt:`
> timestamp before image build; it's invoked by the release workflow in
> `.github/workflows/release.yml` and should not be run on PR branches
> (it would dirty the working tree and fail the drift gate).

## Build/push catalog index

```bash
# Build catalog index containing the bundle
make catalog-build BUNDLE_IMGS=quay.io/your-org/neo4j-operator-bundle:v0.0.1 \
  CATALOG_IMG=quay.io/your-org/neo4j-operator-catalog:v0.0.1

make catalog-push CATALOG_IMG=quay.io/your-org/neo4j-operator-catalog:v0.0.1
```

## Install via OLM on OpenShift

```bash
# Create CatalogSource in openshift-marketplace (edit image)
oc apply -f config/samples/olm/catalogsource.yaml

# Create operator namespace if not present
oc new-project neo4j-operator-system || true

# Create Subscription (edit channel/name if you changed defaults)
oc apply -f config/samples/olm/subscription.yaml
```

Default bundle channels can be set via `CHANNELS`/`DEFAULT_CHANNEL` when running `make bundle`.

## OpenShift smoke testing (manual)

OLM/OperatorHub is a supported **manual** install path; it is not exercised in
CI (a smoke test needs an OpenShift cluster, which standard GitHub runners can't
provide). To validate the bundle on OpenShift, build and push the bundle and
catalog (`make bundle bundle-build bundle-push catalog-build catalog-push`),
then install via the sample `CatalogSource`/`Subscription` under
`config/samples/olm/`. OpenShift Local (CRC) works for a local check on a
virtualization-capable host; a remote OpenShift cluster works equally well.
