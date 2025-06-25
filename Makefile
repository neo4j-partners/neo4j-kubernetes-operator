# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# neo4j.com/neo4j-operator-bundle:$VERSION and neo4j.com/neo4j-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= neo4j.com/neo4j-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.39.1
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: test-setup ## Run tests with clean environment
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: test-setup ## Run e2e tests with clean environment
	go test -v -race -coverprofile=coverage-e2e.out ./test/e2e/...
	go tool cover -html=coverage-e2e.out -o coverage-e2e.html

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-staticcheck
lint-staticcheck: staticcheck ## Run staticcheck static analysis
	$(STATICCHECK) ./...

.PHONY: lint-comprehensive
lint-comprehensive: golangci-lint staticcheck ## Run comprehensive static analysis (golangci-lint + staticcheck)
	@echo "🔍 Running comprehensive static analysis..."
	@echo "📋 Running golangci-lint..."
	$(GOLANGCI_LINT) run
	@echo "🔬 Running staticcheck..."
	$(STATICCHECK) ./...
	@echo "✅ Static analysis completed successfully!"

.PHONY: lint-lenient
lint-lenient: golangci-lint staticcheck ## Run lenient static analysis with higher thresholds
	@echo "🔍 Running lenient static analysis..."
	@echo "📋 Running golangci-lint with lenient settings..."
	$(GOLANGCI_LINT) run --config .golangci.yml
	@echo "🔬 Running staticcheck..."
	$(STATICCHECK) ./...
	@echo "✅ Lenient static analysis completed successfully!"

.PHONY: lint-all
lint-all: lint-comprehensive ## Alias for comprehensive static analysis

.PHONY: lint-fix-all
lint-fix-all: golangci-lint staticcheck ## Run all linters with auto-fixing where possible
	@echo "🔧 Running linters with auto-fixing..."
	$(GOLANGCI_LINT) run --fix
	@echo "📝 Note: staticcheck issues need manual fixing"
	$(STATICCHECK) ./...

format: golangci-lint ## Format Go code using golangci-lint formatters
	$(GOLANGCI_LINT) fmt

lint-fast: golangci-lint ## Run fast golangci-lint for pre-commit hooks
	$(GOLANGCI_LINT) run --config .golangci-precommit.yml --fix --timeout=2m

##@ Development

.PHONY: setup-dev
setup-dev: ## Set up development environment with all tools and dependencies.
	@echo "Setting up development environment..."
	@hack/setup-dev.sh

.PHONY: install-hooks
install-hooks: ## Install pre-commit hooks.
	@echo "Installing pre-commit hooks..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit install; \
		pre-commit install --hook-type commit-msg; \
		echo "Pre-commit hooks installed successfully"; \
	else \
		echo "pre-commit not found. Run 'make setup-dev' first."; \
		exit 1; \
	fi

.PHONY: dev-cluster
dev-cluster: ## Create a Kind cluster for development.
	@echo "Creating development cluster..."
	@echo "Setting up Kind cluster prerequisites..."
	@./scripts/setup-kind-dirs.sh
	@if ! kind get clusters | grep -q "neo4j-operator-dev"; then \
		kind create cluster --config hack/kind-config.yaml; \
		echo "Waiting for cluster to be ready..."; \
		kubectl wait --for=condition=ready node --all --timeout=300s; \
		echo "Installing cert-manager..."; \
		kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml; \
		kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s; \
		echo "Development cluster ready!"; \
	else \
		echo "Development cluster already exists"; \
	fi

.PHONY: dev-cluster-delete
dev-cluster-delete: ## Delete the Kind development cluster.
	@echo "Deleting development cluster..."
	@kind delete cluster --name neo4j-operator-dev || true

.PHONY: validate-cluster
validate-cluster: ## Validate cluster connectivity and health.
	@echo "Validating cluster connectivity..."
	@./scripts/validate-cluster.sh --verbose

.PHONY: validate-cluster-kind
validate-cluster-kind: ## Validate Kind cluster connectivity and health.
	@echo "Validating Kind cluster connectivity..."
	@./scripts/validate-cluster.sh --type kind --name neo4j-operator-test --verbose

.PHONY: validate-cluster-openshift
validate-cluster-openshift: ## Validate OpenShift cluster connectivity and health.
	@echo "Validating OpenShift cluster connectivity..."
	@if [ -z "$(OPENSHIFT_SERVER)" ] || [ -z "$(OPENSHIFT_TOKEN)" ]; then \
		echo "Error: OPENSHIFT_SERVER and OPENSHIFT_TOKEN environment variables are required"; \
		exit 1; \
	fi
	@./scripts/validate-cluster.sh --type openshift --server "$(OPENSHIFT_SERVER)" --token "$(OPENSHIFT_TOKEN)" --verbose

.PHONY: validate-cluster-remote
validate-cluster-remote: ## Validate remote cluster connectivity and health.
	@echo "Validating remote cluster connectivity..."
	@./scripts/validate-cluster.sh --type remote --verbose

.PHONY: cluster-info
cluster-info: ## Display cluster information and status.
	@echo "=== Cluster Information ==="
	@kubectl cluster-info
	@echo ""
	@echo "=== Node Status ==="
	@kubectl get nodes -o wide
	@echo ""
	@echo "=== System Pods ==="
	@kubectl get pods -n kube-system
	@echo ""
	@echo "=== API Server Health ==="
	@kubectl get --raw /healthz

.PHONY: cluster-health
cluster-health: ## Check cluster health and component status.
	@echo "=== Cluster Health Check ==="
	@echo "Checking API server connectivity..."
	@kubectl get --raw /healthz || (echo "❌ API server not accessible" && exit 1)
	@echo "✅ API server is accessible"
	@echo ""
	@echo "Checking node readiness..."
	@kubectl wait --for=condition=ready nodes --all --timeout=60s || (echo "❌ Not all nodes are ready" && exit 1)
	@echo "✅ All nodes are ready"
	@echo ""
	@echo "Checking core components..."
	@kubectl get pods -n kube-system --no-headers | grep -E "(kube-apiserver|kube-controller-manager|kube-scheduler|etcd)" || echo "⚠️  Some core components may not be visible"
	@echo "✅ Cluster health check completed"

.PHONY: dev-run
dev-run: ## Run the operator locally for development.
	@hack/dev-run.sh

.PHONY: dev-run-debug
dev-run-debug: ## Run the operator locally with debugger.
	@hack/dev-run.sh --debug

.PHONY: dev-run-hot
dev-run-hot: ## Run the operator locally with hot reload.
	@hack/dev-run.sh --hot-reload

.PHONY: dev-run-webhooks
dev-run-webhooks: ## Run the operator locally with webhooks enabled.
	@hack/dev-run.sh --webhooks

.PHONY: dev-cleanup
dev-cleanup: ## Clean up development environment.
	@hack/cleanup-dev.sh

.PHONY: dev-cleanup-all
dev-cleanup-all: ## Clean up everything including cluster.
	@hack/cleanup-dev.sh --cluster --logs --force

.PHONY: test-samples
test-samples: ## Test sample Neo4j configurations.
	@hack/test-samples.sh

.PHONY: test-topology
test-topology: ## Test topology-aware placement features.
	@echo "Testing topology-aware placement..."
	@if kubectl get nodes -l topology.kubernetes.io/zone >/dev/null 2>&1; then \
		kubectl apply -f config/samples/topology-aware-cluster.yaml; \
		echo "Waiting for cluster to be ready..."; \
		kubectl wait --for=condition=Ready neo4jenterprisecluster/neo4j-topology-aware --timeout=300s || true; \
		./scripts/validate-topology.sh neo4j-topology-aware neo4j; \
		kubectl delete -f config/samples/topology-aware-cluster.yaml --ignore-not-found; \
	else \
		echo "No zones found in cluster. Creating multi-zone test cluster..."; \
		$(MAKE) dev-cluster-multizone; \
		$(MAKE) test-topology; \
	fi

.PHONY: dev-cluster-multizone
dev-cluster-multizone: ## Create a multi-zone Kind cluster for topology testing.
	@echo "Creating multi-zone development cluster..."
	@echo "Setting up Kind cluster prerequisites..."
	@./scripts/setup-kind-dirs.sh
	@if ! kind get clusters | grep -q "neo4j-operator-multizone"; then \
		cat > /tmp/kind-multizone-config.yaml <<EOF; \
kind: Cluster; \
apiVersion: kind.x-k8s.io/v1alpha4; \
name: neo4j-operator-multizone; \
nodes:; \
- role: control-plane; \
  labels:; \
    topology.kubernetes.io/zone: zone-a; \
- role: worker; \
  labels:; \
    topology.kubernetes.io/zone: zone-a; \
- role: worker; \
  labels:; \
    topology.kubernetes.io/zone: zone-b; \
- role: worker; \
  labels:; \
    topology.kubernetes.io/zone: zone-c; \
EOF; \
		kind create cluster --config /tmp/kind-multizone-config.yaml; \
		kubectl wait --for=condition=ready node --all --timeout=300s; \
		echo "Multi-zone development cluster ready!"; \
	else \
		echo "Multi-zone development cluster already exists"; \
	fi

.PHONY: debug
debug: ## Build and run with delve debugger.
	@echo "Building with debug symbols..."
	@go build -gcflags="all=-N -l" -o bin/manager cmd/main.go
	@echo "Starting debugger on :2345..."
	@dlv exec bin/manager --listen=:2345 --headless=true --api-version=2 --accept-multiclient

##@ Testing

.PHONY: test-unit
test-unit: manifests generate fmt vet envtest ## Run unit tests (no cluster required).
	@echo "Running unit tests (no cluster required)..."
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e | grep -v /integration) -coverprofile cover.out -race -v

.PHONY: test-unit-only
test-unit-only: ## Run only unit tests that don't require a cluster.
	@echo "Running unit tests only (no cluster required)..."
	go test -v -race ./internal/controller/... -timeout=10m
	go test -v -race ./internal/webhooks/... -timeout=5m
	go test -v -race ./internal/neo4j/... -timeout=10m

.PHONY: test-webhooks
test-webhooks: manifests generate fmt vet envtest ## Run webhook tests (no cluster required).
	@echo "Running webhook tests (no cluster required)..."
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./internal/webhooks/... -v -timeout=10m

.PHONY: test-security
test-security: ## Run security-focused tests (no cluster required).
	@echo "Running security tests (no cluster required)..."
	go test ./internal/controller/security_coordinator_test.go -v
	go test ./internal/webhooks/... -v -run=".*Security.*"

.PHONY: test-neo4j-client
test-neo4j-client: ## Run Neo4j client tests (no cluster required).
	@echo "Running Neo4j client tests (no cluster required)..."
	go test ./internal/neo4j/... -v

.PHONY: test-controllers
test-controllers: ## Run controller tests (no cluster required).
	@echo "Running controller tests (no cluster required)..."
	go test ./internal/controller/... -v -run="Test.*" -timeout=10m

.PHONY: test-integration
test-integration: test-setup ## Run integration tests (requires cluster).
	@echo "Checking cluster availability for integration tests..."
	@if ./scripts/check-cluster.sh --verbose; then \
		echo "Cluster is available, running integration tests..."; \
		go test -v -race -coverprofile=coverage-integration.out ./test/integration/...; \
		go tool cover -html=coverage-integration.out -o coverage-integration.html; \
	else \
		echo "❌ No cluster available for integration tests"; \
		echo "💡 Run 'make dev-cluster' to create a local cluster"; \
		echo "💡 Or set up a remote cluster and configure kubectl"; \
		echo "💡 Skipping integration tests..."; \
		exit 0; \
	fi

.PHONY: test-e2e
test-e2e: test-setup ## Run e2e tests (requires cluster).
	@echo "Checking cluster availability for e2e tests..."
	@if ./scripts/check-cluster.sh --verbose; then \
		echo "Cluster is available, running e2e tests..."; \
		go test -v -race -coverprofile=coverage-e2e.out ./test/e2e/...; \
		go tool cover -html=coverage-e2e.out -o coverage-e2e.html; \
	else \
		echo "❌ No cluster available for e2e tests"; \
		echo "💡 Run 'make dev-cluster' to create a local cluster"; \
		echo "💡 Or set up a remote cluster and configure kubectl"; \
		echo "💡 Skipping e2e tests..."; \
		exit 0; \
	fi

.PHONY: test-local
test-local: ## Run tests against local Kubernetes cluster.
	@echo "Checking local cluster availability..."
	@if ./scripts/check-cluster.sh --type kind --verbose; then \
		echo "Local cluster is available, running tests..."; \
		CLUSTER_TYPE=local go test ./test/integration/... -v -timeout=15m; \
	else \
		echo "❌ No local cluster available"; \
		echo "💡 Run 'make dev-cluster' to create a local cluster"; \
		exit 1; \
	fi

.PHONY: test-cloud
test-cloud: test-eks test-gke test-aks ## Run all cloud provider tests (requires cloud clusters).

.PHONY: test-eks
test-eks: ## Run EKS-specific tests (requires EKS cluster).
	@echo "Checking EKS cluster availability..."
	@if [ "$(CLUSTER_TYPE)" = "eks" ] && ./scripts/check-cluster.sh --type remote --verbose; then \
		echo "EKS cluster is available, running tests..."; \
		go test ./test/cloud/eks/... -v -timeout=20m; \
	else \
		echo "❌ EKS cluster not available or CLUSTER_TYPE not set to 'eks'"; \
		echo "💡 Set CLUSTER_TYPE=eks and ensure kubectl is configured for EKS"; \
		echo "💡 Skipping EKS tests..."; \
		exit 0; \
	fi

.PHONY: test-gke
test-gke: ## Run GKE-specific tests (requires GKE cluster).
	@echo "Checking GKE cluster availability..."
	@if [ "$(CLUSTER_TYPE)" = "gke" ] && ./scripts/check-cluster.sh --type remote --verbose; then \
		echo "GKE cluster is available, running tests..."; \
		go test ./test/cloud/gke/... -v -timeout=20m; \
	else \
		echo "❌ GKE cluster not available or CLUSTER_TYPE not set to 'gke'"; \
		echo "💡 Set CLUSTER_TYPE=gke and ensure kubectl is configured for GKE"; \
		echo "💡 Skipping GKE tests..."; \
		exit 0; \
	fi

.PHONY: test-aks
test-aks: ## Run AKS-specific tests (requires AKS cluster).
	@echo "Checking AKS cluster availability..."
	@if [ "$(CLUSTER_TYPE)" = "aks" ] && ./scripts/check-cluster.sh --type remote --verbose; then \
		echo "AKS cluster is available, running tests..."; \
		go test ./test/cloud/aks/... -v -timeout=20m; \
	else \
		echo "❌ AKS cluster not available or CLUSTER_TYPE not set to 'aks'"; \
		echo "💡 Set CLUSTER_TYPE=aks and ensure kubectl is configured for AKS"; \
		echo "💡 Skipping AKS tests..."; \
		exit 0; \
	fi

.PHONY: test-backup-restore
test-backup-restore: ## Run backup and restore tests (requires cluster).
	@echo "Checking cluster availability for backup/restore tests..."
	@if ./scripts/check-cluster.sh --verbose; then \
		echo "Cluster is available, running backup/restore tests..."; \
		go test ./internal/controller/neo4jbackup_controller_test.go -v; \
		go test ./internal/controller/neo4jrestore_controller_test.go -v; \
	else \
		echo "❌ No cluster available for backup/restore tests"; \
		echo "💡 Run 'make dev-cluster' to create a local cluster"; \
		echo "💡 Or set up a remote cluster and configure kubectl"; \
		echo "💡 Skipping backup/restore tests..."; \
		exit 0; \
	fi

.PHONY: test-comprehensive
test-comprehensive: test-unit test-integration test-webhooks test-security ## Run comprehensive test suite (integration tests conditional on cluster).

.PHONY: test-ci
test-ci: test-unit test-webhooks test-security test-integration ## Run CI test suite (integration tests conditional on cluster).

.PHONY: test-all
test-all: test-unit test-integration test-e2e ## Run all tests (integration and e2e tests conditional on cluster).

.PHONY: test-no-cluster
test-no-cluster: test-unit test-webhooks test-security test-neo4j-client test-controllers ## Run all tests that don't require a cluster.

.PHONY: test-with-cluster
test-with-cluster: test-integration test-e2e test-backup-restore ## Run all tests that require a cluster.

.PHONY: test-coverage-comprehensive
test-coverage-comprehensive: ## Generate comprehensive test coverage report (conditional on cluster).
	@echo "Generating comprehensive coverage report..."
	@echo "Running unit tests (no cluster required)..."
	go test -coverprofile=coverage-comprehensive.out -coverpkg=./... \
		./internal/controller/... \
		./internal/webhooks/... \
		./internal/neo4j/...
	@if ./scripts/check-cluster.sh; then \
		echo "Cluster is available, running integration tests for coverage..."; \
		go test -coverprofile=coverage-integration-temp.out -coverpkg=./... ./test/integration/...; \
		cat coverage-integration-temp.out >> coverage-comprehensive.out; \
		rm coverage-integration-temp.out; \
	else \
		echo "No cluster available, skipping integration tests for coverage..."; \
	fi
	go tool cover -html=coverage-comprehensive.out -o coverage-comprehensive.html
	@echo "Coverage report generated: coverage-comprehensive.html"

.PHONY: test-race
test-race: ## Run tests with race detection (no cluster required).
	@echo "Running tests with race detection (no cluster required)..."
	go test -race ./internal/controller/... ./internal/webhooks/... ./internal/neo4j/...

.PHONY: test-parallel
test-parallel: ## Run tests in parallel for faster execution (no cluster required).
	@echo "Running tests in parallel (no cluster required)..."
	go test -parallel 4 ./internal/controller/... ./internal/webhooks/... ./internal/neo4j/...

.PHONY: test-cloud-setup
test-cloud-setup: ## Setup environment for cloud provider tests.
	@echo "Setting up cloud test environment..."
	@echo "Available cloud providers: eks, gke, aks"
	@echo "Set CLUSTER_TYPE environment variable to run specific cloud tests"
	@echo "Example: CLUSTER_TYPE=eks make test-eks"
	@echo ""
	@echo "Required environment variables:"
	@echo "For EKS:"
	@echo "  - S3_BACKUP_BUCKET: S3 bucket for backups"
	@echo "  - AWS_BACKUP_ROLE_ARN: IAM role ARN for IRSA"
	@echo ""
	@echo "For GKE:"
	@echo "  - GCS_BACKUP_BUCKET: GCS bucket for backups"
	@echo "  - GCP_SERVICE_ACCOUNT: Service account for Workload Identity"
	@echo ""
	@echo "For AKS:"
	@echo "  - AZURE_STORAGE_CONTAINER: Azure storage container"
	@echo "  - AZURE_CLIENT_ID: Client ID for Azure Workload Identity"

.PHONY: coverage
coverage: test-unit ## Generate and view test coverage report (no cluster required).
	@echo "Generating coverage report..."
	@go tool cover -html=cover.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@if command -v open >/dev/null 2>&1; then open coverage.html; fi

.PHONY: benchmark
benchmark: ## Run benchmarks (no cluster required).
	@echo "Running benchmarks (no cluster required)..."
	@go test -bench=. -benchmem ./...

##@ Code Quality

.PHONY: pre-commit
pre-commit: ## Run pre-commit hooks on all files.
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit run --all-files; \
	else \
		echo "pre-commit not found. Run 'make setup-dev' first."; \
		exit 1; \
	fi

.PHONY: security
security: ## Run security scans.
	@echo "Running security scans..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "Installing gosec..."; \
		go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
		gosec ./...; \
	fi

.PHONY: check-licenses
check-licenses: ## Check license compatibility.
	@echo "Checking license compatibility..."
	@if command -v go-licenses >/dev/null 2>&1; then \
		go-licenses check ./...; \
	else \
		echo "Installing go-licenses..."; \
		go install github.com/google/go-licenses@latest; \
		go-licenses check ./...; \
	fi

.PHONY: tidy
tidy: ## Tidy go modules.
	@echo "Tidying go modules..."
	@go mod tidy
	@go mod verify

.PHONY: clean
clean: ## Clean build artifacts and temporary files.
	@echo "Cleaning build artifacts..."
	@chmod -R +w bin/ 2>/dev/null || true
	@rm -rf bin/
	@rm -rf tmp/
	@rm -rf dist/
	@rm -f cover.out
	@rm -f coverage.html
	@rm -f results.sarif
	@rm -f build-errors.log
	@rm -f .air.toml

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: build-plugin
build-plugin: ## Build kubectl plugin
	cd cmd/kubectl-neo4j && make build

.PHONY: install-plugin
install-plugin: build-plugin ## Install kubectl plugin locally
	cd cmd/kubectl-neo4j && make install

.PHONY: build-debug
build-debug: manifests generate ## Build manager binary with debug symbols.
	go build -gcflags="all=-N -l" -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name neo4j-operator-builder
	$(CONTAINER_TOOL) buildx use neo4j-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm neo4j-operator-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: deploy-ci
deploy-ci: manifests kustomize ## Deploy controller to the K8s cluster with webhooks disabled for CI.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/ci | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
STATICCHECK = $(LOCALBIN)/staticcheck

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.3
CONTROLLER_TOOLS_VERSION ?= v0.16.1
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.64.8
STATICCHECK_VERSION ?= 2025.1.1

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: staticcheck
staticcheck: $(STATICCHECK) ## Download staticcheck locally if necessary.
$(STATICCHECK): $(LOCALBIN)
	$(call go-install-tool,$(STATICCHECK),honnef.co/go/tools/cmd/staticcheck,$(STATICCHECK_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: dev-init
dev-init: ## Initialize complete development environment with all tools and services.
	@echo "🚀 Initializing Neo4j Operator development environment..."
	@scripts/dev-environment.sh init

.PHONY: dev-dashboard
dev-dashboard: ## Show development environment dashboard.
	@scripts/dev-environment.sh dashboard

.PHONY: dev-start
dev-start: ## Start development environment.
	@scripts/dev-environment.sh start

.PHONY: dev-start-fast
dev-start-fast: ## Start development environment with fast optimizations.
	@echo "🚀 Starting optimized development environment..."
	@go run cmd/main.go \
		--mode=dev \
		--zap-devel=true \
		--zap-log-level=info \
		--leader-elect=false \
		--enable-webhooks=false \
		--controllers=cluster

.PHONY: dev-start-minimal
dev-start-minimal: ## Start development environment with minimal configuration for fastest startup.
	@echo "⚡ Starting MINIMAL development environment for fastest startup..."
	@go run cmd/main.go \
		--mode=minimal \
		--zap-devel=true \
		--zap-log-level=info \
		--namespace=default \
		--sync-period=30s

.PHONY: dev-startup-help
dev-startup-help: ## Show all available development startup options and their performance characteristics.
	@scripts/startup-help.sh

.PHONY: dev-stop
dev-stop: ## Stop development environment.
	@scripts/dev-environment.sh stop

.PHONY: dev-restart
dev-restart: ## Restart development environment.
	@scripts/dev-environment.sh restart

.PHONY: dev-logs
dev-logs: ## View development logs.
	@scripts/dev-environment.sh logs

.PHONY: dev-debug
dev-debug: ## Start interactive debug session.
	@scripts/dev-environment.sh debug

.PHONY: dev-clean
dev-clean: ## Clean development environment (processes, files, ports).
	@scripts/dev-environment.sh clean

.PHONY: dev-clean-all
dev-clean-all: ## Clean everything (processes, files, ports, Docker, Go cache).
	@scripts/dev-environment.sh clean --all

.PHONY: dev-services
dev-services: ## Start development services (Neo4j, Prometheus, etc.).
	@echo "Starting development services..."
	@docker-compose -f docker-compose.dev.yml up -d
	@echo "Services started. Access:"
	@echo "  Neo4j Browser: http://localhost:7474"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  Grafana: http://localhost:3000 (admin/admin)"

.PHONY: dev-services-cluster
dev-services-cluster: ## Start Neo4j cluster for testing.
	@echo "Starting Neo4j cluster..."
	@docker-compose -f docker-compose.dev.yml --profile cluster up -d
	@echo "Cluster started. Access nodes at:"
	@echo "  Core 1: http://localhost:7475"
	@echo "  Core 2: http://localhost:7476"
	@echo "  Core 3: http://localhost:7477"

.PHONY: dev-services-monitoring
dev-services-monitoring: ## Start monitoring stack.
	@echo "Starting monitoring services..."
	@docker-compose -f docker-compose.dev.yml --profile monitoring up -d

.PHONY: dev-services-stop
dev-services-stop: ## Stop all development services.
	@echo "Stopping development services..."
	@docker-compose -f docker-compose.dev.yml down

.PHONY: dev-services-clean
dev-services-clean: ## Clean development services and volumes.
	@echo "Cleaning development services..."
	@docker-compose -f docker-compose.dev.yml down -v
	@docker system prune -f

##@ Code Quality

.PHONY: quality
quality: ## Run comprehensive code quality analysis.
	@scripts/code-quality.sh all

.PHONY: quality-quick
quality-quick: ## Run quick code quality checks.
	@scripts/code-quality.sh quick

.PHONY: quality-format
quality-format: ## Format code with advanced formatters.
	@scripts/code-quality.sh format

.PHONY: quality-lint
quality-lint: ## Run comprehensive linting.
	@scripts/code-quality.sh lint

.PHONY: quality-security
quality-security: ## Run security analysis.
	@scripts/code-quality.sh security

.PHONY: quality-deps
quality-deps: ## Analyze dependencies.
	@scripts/code-quality.sh deps

.PHONY: quality-metrics
quality-metrics: ## Generate code metrics.
	@scripts/code-quality.sh metrics

.PHONY: quality-report
quality-report: ## Generate and open quality report.
	@scripts/code-quality.sh report
	@scripts/code-quality.sh open

.PHONY: quality-install
quality-install: ## Install code quality tools.
	@scripts/code-quality.sh install

##@ Testing Framework

.PHONY: test-framework-init
test-framework-init: ## Initialize testing framework.
	@scripts/testing-framework.sh setup

.PHONY: test-unit-comprehensive
test-unit-comprehensive: ## Run comprehensive unit tests.
	@scripts/testing-framework.sh unit

.PHONY: test-integration-comprehensive
test-integration-comprehensive: ## Run comprehensive integration tests.
	@scripts/testing-framework.sh integration

.PHONY: test-e2e-comprehensive
test-e2e-comprehensive: ## Run comprehensive e2e tests.
	@scripts/testing-framework.sh e2e

.PHONY: test-ginkgo
test-ginkgo: ## Run Ginkgo BDD tests.
	@scripts/testing-framework.sh ginkgo

.PHONY: test-benchmark
test-benchmark: ## Run benchmark tests.
	@scripts/testing-framework.sh benchmark

.PHONY: test-security-comprehensive
test-security-comprehensive: ## Run security tests.
	@scripts/testing-framework.sh security

.PHONY: test-performance
test-performance: ## Run performance tests.
	@scripts/testing-framework.sh performance

.PHONY: test-chaos
test-chaos: ## Run chaos engineering tests.
	@scripts/testing-framework.sh chaos

.PHONY: test-all-comprehensive
test-all-comprehensive: ## Run all comprehensive tests.
	@scripts/testing-framework.sh all

.PHONY: test-quick-comprehensive
test-quick-comprehensive: ## Run quick test suite.
	@scripts/testing-framework.sh quick

.PHONY: test-report
test-report: ## Generate test report.
	@scripts/testing-framework.sh report

.PHONY: test-summary
test-summary: ## Show test results summary.
	@scripts/testing-framework.sh summary

.PHONY: test-clean
test-clean: ## Clean test artifacts.
	@scripts/testing-framework.sh clean

##@ Documentation

.PHONY: docs-serve
docs-serve: ## Serve documentation locally.
	@echo "Starting documentation server..."
	@if command -v mkdocs >/dev/null 2>&1; then \
		mkdocs serve; \
	else \
		echo "mkdocs not found. Install with: pip install mkdocs mkdocs-material"; \
	fi

.PHONY: docs-build
docs-build: ## Build documentation.
	@echo "Building documentation..."
	@if command -v mkdocs >/dev/null 2>&1; then \
		mkdocs build; \
	else \
		echo "mkdocs not found. Install with: pip install mkdocs mkdocs-material"; \
	fi

.PHONY: docs-api
docs-api: ## Generate API documentation.
	@echo "Generating API documentation..."
	@go doc -all ./api/... > docs/api-reference.md
	@echo "API documentation generated in docs/api-reference.md"

.PHONY: docs-crd
docs-crd: ## Generate CRD documentation.
	@echo "Generating CRD documentation..."
	@controller-gen crd:generateEmbeddedObjectMeta=true paths="./..." output:crd:artifacts:config=config/crd/bases
	@echo "CRD documentation updated"

##@ Development Utilities

.PHONY: tools-install
tools-install: ## Install all development tools.
	@echo "Installing development tools..."
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/go-delve/delve/cmd/dlv@latest
	@go install github.com/air-verse/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	@go install github.com/onsi/ginkgo/v2/ginkgo@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@go install github.com/vektra/mockery/v2@latest
	@go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@echo "Development tools installed successfully"

.PHONY: tools-update
tools-update: ## Update all development tools.
	@echo "Updating development tools..."
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/go-delve/delve/cmd/dlv@latest
	@go install github.com/air-verse/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	@go install github.com/onsi/ginkgo/v2/ginkgo@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@go install github.com/vektra/mockery/v2@latest
	@go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@echo "Development tools updated successfully"

.PHONY: mock-generate
mock-generate: ## Generate mocks for testing.
	@echo "Generating mocks..."
	@go generate ./...
	@mockery --all --output=mocks --case=underscore
	@echo "Mocks generated successfully"

.PHONY: proto-generate
proto-generate: ## Generate protobuf files (if any).
	@echo "Generating protobuf files..."
	@if find . -name "*.proto" | grep -q .; then \
		protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative *.proto; \
		echo "Protobuf files generated"; \
	else \
		echo "No protobuf files found"; \
	fi

.PHONY: version-bump
version-bump: ## Bump version (usage: make version-bump VERSION=v1.2.3).
	@if [ -z "$(VERSION)" ]; then \
		echo "Usage: make version-bump VERSION=v1.2.3"; \
		exit 1; \
	fi
	@echo "Bumping version to $(VERSION)..."
	@sed -i.bak 's/VERSION ?= .*/VERSION ?= $(VERSION)/' Makefile
	@rm -f Makefile.bak
	@git add Makefile
	@git commit -m "Bump version to $(VERSION)"
	@git tag $(VERSION)
	@echo "Version bumped to $(VERSION) and tagged"

.PHONY: changelog
changelog: ## Generate changelog.
	@echo "Generating changelog..."
	@if command -v git-chglog >/dev/null 2>&1; then \
		git-chglog --output CHANGELOG.md; \
		echo "Changelog generated in CHANGELOG.md"; \
	else \
		echo "git-chglog not found. Install with: go install github.com/git-chglog/git-chglog/cmd/git-chglog@latest"; \
	fi

##@ Performance & Profiling

.PHONY: profile-cpu
profile-cpu: ## Run CPU profiling.
	@echo "Running CPU profiling..."
	@go test -cpuprofile=cpu.prof -bench=. ./...
	@go tool pprof cpu.prof

.PHONY: profile-mem
profile-mem: ## Run memory profiling.
	@echo "Running memory profiling..."
	@go test -memprofile=mem.prof -bench=. ./...
	@go tool pprof mem.prof

.PHONY: profile-trace
profile-trace: ## Run execution tracing.
	@echo "Running execution tracing..."
	@go test -trace=trace.out -bench=. ./...
	@go tool trace trace.out

.PHONY: profile-block
profile-block: ## Run blocking profiling.
	@echo "Running blocking profiling..."
	@go test -blockprofile=block.prof -bench=. ./...
	@go tool pprof block.prof

.PHONY: profile-mutex
profile-mutex: ## Run mutex profiling.
	@echo "Running mutex profiling..."
	@go test -mutexprofile=mutex.prof -bench=. ./...
	@go tool pprof mutex.prof

.PHONY: profile-clean
profile-clean: ## Clean profiling files.
	@echo "Cleaning profiling files..."
	@rm -f *.prof *.out
	@echo "Profiling files cleaned"

##@ Container Development

.PHONY: container-dev
container-dev: ## Build development container.
	@echo "Building development container..."
	@docker build -f Dockerfile.dev -t neo4j-operator:dev .

.PHONY: container-debug
container-debug: ## Build debug container with delve.
	@echo "Building debug container..."
	@docker build -f Dockerfile.debug -t neo4j-operator:debug .

.PHONY: container-test
container-test: ## Run tests in container.
	@echo "Running tests in container..."
	@docker run --rm -v $(PWD):/workspace neo4j-operator:dev make test

.PHONY: container-shell
container-shell: ## Start interactive shell in development container.
	@echo "Starting development container shell..."
	@docker run --rm -it -v $(PWD):/workspace -v /var/run/docker.sock:/var/run/docker.sock neo4j-operator:dev /bin/bash

##@ Monitoring & Observability

.PHONY: metrics-scrape
metrics-scrape: ## Scrape metrics from local operator.
	@echo "Scraping metrics..."
	@curl -s http://localhost:8080/metrics | grep neo4j

.PHONY: health-check
health-check: ## Check operator health.
	@echo "Checking operator health..."
	@curl -s http://localhost:8081/healthz
	@curl -s http://localhost:8081/readyz

.PHONY: logs-follow
logs-follow: ## Follow operator logs.
	@echo "Following operator logs..."
	@kubectl logs -f deployment/neo4j-operator-controller-manager -n neo4j-operator-system || \
		tail -f logs/operator-*.log 2>/dev/null || \
		echo "No logs found. Is the operator running?"

.PHONY: events-watch
events-watch: ## Watch Kubernetes events.
	@echo "Watching Kubernetes events..."
	@kubectl get events --watch --all-namespaces | grep neo4j

##@ Git Workflow

.PHONY: git-hooks-install
git-hooks-install: ## Install git hooks.
	@echo "Installing git hooks..."
	@pre-commit install
	@pre-commit install --hook-type commit-msg
	@echo "Git hooks installed successfully"

.PHONY: git-hooks-update
git-hooks-update: ## Update git hooks.
	@echo "Updating git hooks..."
	@pre-commit autoupdate
	@echo "Git hooks updated successfully"

.PHONY: git-hooks-run
git-hooks-run: ## Run git hooks on all files.
	@echo "Running git hooks on all files..."
	@pre-commit run --all-files

.PHONY: commit-lint
commit-lint: ## Lint commit messages.
	@echo "Linting commit messages..."
	@if command -v commitlint >/dev/null 2>&1; then \
		commitlint --from HEAD~1 --to HEAD --verbose; \
	else \
		echo "commitlint not found. Install with: npm install -g @commitlint/cli @commitlint/config-conventional"; \
	fi

##@ IDE Integration

.PHONY: vscode-setup
vscode-setup: ## Setup VS Code configuration.
	@echo "Setting up VS Code configuration..."
	@mkdir -p .vscode
	@echo "VS Code configuration updated. Restart VS Code to apply changes."

.PHONY: vscode-extensions
vscode-extensions: ## Install recommended VS Code extensions.
	@echo "Installing recommended VS Code extensions..."
	@if command -v code >/dev/null 2>&1; then \
		code --install-extension golang.go; \
		code --install-extension ms-kubernetes-tools.vscode-kubernetes-tools; \
		code --install-extension redhat.vscode-yaml; \
		code --install-extension eamodio.gitlens; \
		echo "VS Code extensions installed"; \
	else \
		echo "VS Code not found in PATH"; \
	fi

.PHONY: jetbrains-setup
jetbrains-setup: ## Setup JetBrains IDE configuration.
	@echo "Setting up JetBrains IDE configuration..."
	@mkdir -p .idea
	@echo "JetBrains IDE configuration updated"

##@ Cleanup

.PHONY: clean-all
clean-all: ## Clean everything (artifacts, containers, volumes).
	@echo "Cleaning everything..."
	@make clean
	@make dev-services-clean
	@make test-clean
	@make profile-clean
	@docker system prune -af
	@echo "Everything cleaned"

.PHONY: reset-dev
reset-dev: ## Reset development environment.
	@echo "Resetting development environment..."
	@make clean-all
	@make dev-init
	@echo "Development environment reset"

##@ Help Enhancement

.PHONY: help-dev
help-dev: ## Show comprehensive development help.
	@echo ""
	@echo "🚀 Neo4j Operator Development Guide"
	@echo ""
	@echo "Quick Start:"
	@echo "  make dev-init          # Initialize complete development environment"
	@echo "  make dev-dashboard     # Show development dashboard"
	@echo "  make dev-services      # Start Neo4j and monitoring services"
	@echo "  make dev-start         # Start operator in development mode"
	@echo ""
	@echo "Development Workflow:"
	@echo "  1. make dev-init       # One-time setup"
	@echo "  2. make dev-services   # Start services"
	@echo "  3. make dev-start      # Start operator"
	@echo "  4. make test-quick     # Run tests"
	@echo "  5. make quality-quick  # Check code quality"
	@echo ""
	@echo "Run 'make help' for all available targets."

.PHONY: test-cleanup
test-cleanup: ## Perform aggressive test environment cleanup
	@echo "🧹 Performing aggressive test environment cleanup..."
	@if [ -f "scripts/test-cleanup.sh" ]; then \
		chmod +x scripts/test-cleanup.sh; \
		./scripts/test-cleanup.sh cleanup; \
	else \
		echo "Warning: cleanup script not found"; \
	fi

.PHONY: test-check
test-check: ## Perform test environment sanity checks
	@echo "🔍 Performing test environment sanity checks..."
	@if [ -f "scripts/test-cleanup.sh" ]; then \
		chmod +x scripts/test-cleanup.sh; \
		./scripts/test-cleanup.sh check; \
	else \
		echo "Warning: cleanup script not found"; \
	fi

.PHONY: test-setup
test-setup: test-cleanup ## Set up clean test environment (cleanup + checks)
	@echo "🔧 Setting up clean test environment..."
	@echo "🧹 Performing aggressive cleanup..."
	@if [ -f "scripts/test-cleanup.sh" ]; then \
		chmod +x scripts/test-cleanup.sh; \
		export AGGRESSIVE_CLEANUP=true; \
		export FORCE_CLEANUP=true; \
		export VERBOSE=true; \
		./scripts/test-cleanup.sh cleanup; \
	else \
		echo "Warning: cleanup script not found"; \
	fi
	@echo "🔍 Performing environment checks..."
	@if [ -f "scripts/test-cleanup.sh" ]; then \
		chmod +x scripts/test-cleanup.sh; \
		./scripts/test-cleanup.sh check; \
	else \
		echo "Warning: cleanup script not found"; \
	fi
	@echo "✅ Test environment setup completed"

.PHONY: test-runner
test-runner: ## Run comprehensive tests with cleanup using the test runner script
	@echo "🚀 Running comprehensive test suite with cleanup..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh; \
	else \
		echo "Error: test runner script not found"; \
		exit 1; \
	fi

.PHONY: test-runner-unit
test-runner-unit: ## Run unit tests with cleanup using the test runner script
	@echo "🧪 Running unit tests with cleanup..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh --test-type unit; \
	else \
		echo "Error: test runner script not found"; \
		exit 1; \
	fi

.PHONY: test-runner-integration
test-runner-integration: ## Run integration tests with cleanup using the test runner script
	@echo "🔗 Running integration tests with cleanup..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh --test-type integration; \
	else \
		echo "Error: test runner script not found"; \
		exit 1; \
	fi

.PHONY: test-runner-e2e
test-runner-e2e: ## Run e2e tests with cleanup using the test runner script
	@echo "🌐 Running e2e tests with cleanup..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh --test-type e2e; \
	else \
		echo "Error: test runner script not found"; \
		exit 1; \
	fi

.PHONY: test-runner-parallel
test-runner-parallel: ## Run all tests in parallel with cleanup using the test runner script
	@echo "⚡ Running all tests in parallel with cleanup..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh --test-type all --parallel; \
	else \
		echo "Error: test runner script not found"; \
		exit 1; \
	fi

.PHONY: test-cluster-configs
test-cluster-configs: ## Test all Kind cluster configurations to ensure they work.
	@echo "Testing Kind cluster configurations..."
	@./scripts/test-cluster-creation.sh

.PHONY: test-ci-cluster
test-ci-cluster: ## Test CI cluster creation with the fixes applied.
	@echo "Testing CI cluster creation..."
	@./scripts/test-ci-cluster.sh

.PHONY: test-minimal-cluster
test-minimal-cluster: ## Test minimal Kind cluster creation without configuration.
	@echo "Testing minimal cluster creation..."
	@./scripts/test-minimal-cluster.sh

.PHONY: test-cgroups-v2
test-cgroups-v2: ## Test cgroups v2 configuration specifically.
	@echo "Testing cgroups v2 configuration..."
	@./scripts/test-cgroups-v2.sh

.PHONY: test-local
test-local: ## Run tests against local Kubernetes cluster.
	@echo "Checking local cluster availability..."
	@if ./scripts/check-cluster.sh --type kind --verbose; then \
		echo "Local cluster is available, running tests..."; \
		CLUSTER_TYPE=local go test ./test/integration/... -v -timeout=15m; \
	else \
		echo "❌ No local cluster available"; \
		echo "💡 Run 'make dev-cluster' to create a local cluster"; \
		exit 1; \
	fi
