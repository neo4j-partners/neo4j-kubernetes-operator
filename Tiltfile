# -*- mode: Python -*-
# Tiltfile for Neo4j Kubernetes Operator
#
# Prerequisites:
#   - Kind cluster running: make dev-cluster
#   - Tilt installed: brew install tilt (macOS) or https://docs.tilt.dev/install.html
#
# Usage:
#   tilt up          # Start dev loop with live reload
#   tilt down        # Stop and clean up
#   tilt up --stream # Start with log streaming in terminal (no browser)

# ---- Configuration ----
# Operator image name (must match kustomize overlay config/overlays/dev)
OPERATOR_IMAGE = 'neo4j-operator:dev'

# Detect host architecture for cross-compilation to Linux
HOST_ARCH = str(local('go env GOARCH', quiet=True)).strip()

# ---- Step 1: Auto-regenerate CRDs and code when api/ changes ----
local_resource(
    'generate',
    cmd='make manifests generate',
    deps=['api/'],
    labels=['codegen'],
)

# ---- Step 2: Install/update CRDs in the cluster ----
local_resource(
    'install-crds',
    cmd='make install',
    deps=['config/crd/bases/'],
    resource_deps=['generate'],
    labels=['codegen'],
)

# ---- Step 3: Compile binary + build image in one step ----
# Combines compile and Docker build so the image is only built after the
# binary exists. Uses a minimal alpine base (not distroless) so Tilt's
# live_update can sync files into the running container.
custom_build(
    OPERATOR_IMAGE,
    command='CGO_ENABLED=0 GOOS=linux GOARCH=' + HOST_ARCH + ' go build -o bin/manager cmd/main.go && docker build -t $EXPECTED_REF -f hack/Dockerfile.tilt bin/',
    deps=['cmd/', 'internal/', 'api/'],
    ignore=['**/*_test.go'],
)

# ---- Step 5: Deploy the operator via kustomize ----
k8s_yaml(kustomize('config/overlays/dev'))

# ---- Step 6: Configure the operator resource in Tilt ----
k8s_resource(
    'neo4j-operator-controller-manager',
    port_forwards=[
        port_forward(8080, 8080, name='metrics'),
        port_forward(8081, 8081, name='health'),
    ],
    resource_deps=['install-crds'],
    labels=['operator'],
)

# ---- Step 7: Run unit tests on change (optional, toggle in Tilt UI) ----
local_resource(
    'unit-tests',
    cmd='go test $(go list ./... | grep -v /e2e | grep -v /integration | grep -v /test/) -count=1 -short',
    deps=['internal/', 'api/'],
    ignore=['**/*_test.go'],
    auto_init=False,  # Don't run on startup; toggle in Tilt UI
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['test'],
)

# ---- Helpful info ----
print("""
=== Neo4j Kubernetes Operator (Tilt) ===

Resources:
  - generate:     Auto-runs 'make manifests generate' on api/ changes
  - install-crds: Applies CRDs to cluster
  - operator:     Compiles (""" + HOST_ARCH + """), builds image, deploys to Kind
  - unit-tests:   Run manually from the Tilt UI

Quick test:
  kubectl create secret generic neo4j-admin-secret --from-literal=username=neo4j --from-literal=password=admin123
  kubectl apply -f examples/standalone/single-node-standalone.yaml

Press space to open the Tilt UI in your browser.
""")
