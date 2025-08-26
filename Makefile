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

##@ Code Generation

.PHONY: manifests
manifests: controller-gen ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-lenient
lint-lenient: golangci-lint ## Run lenient static analysis with higher thresholds
	@echo "🔍 Running lenient static analysis..."
	@echo "📋 Running golangci-lint with lenient settings..."
	$(GOLANGCI_LINT) run --config .golangci.yml
	@echo "✅ Lenient static analysis completed successfully!"

##@ Testing

# Test Environment Management
.PHONY: test-setup
test-setup: ## Setup test environment
	@echo "🔧 Setting up test environment..."
	@./scripts/test-env.sh setup

.PHONY: test-cleanup
test-cleanup: ## Clean up test environment
	@echo "🧹 Cleaning up test environment..."
	@./scripts/test-env.sh cleanup
	@rm -rf test-results coverage logs tmp
	@rm -f test-output.log coverage-*.out coverage-*.html

# Unit Tests
.PHONY: test-unit
test-unit: manifests generate fmt vet envtest ## Run unit tests (no cluster required)
	@echo "🧪 Running unit tests..."
	@mkdir -p coverage
	@./scripts/run-tests-clean.sh env KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e | grep -v /integration | grep -v "/test/webhooks" | grep -v "/test/utils" | grep -v "/test/testutil" | grep -v "/cmd") -coverprofile coverage/coverage-unit.out -race -v

# Webhook tests removed - webhooks migrated to client-side validation


# Integration Tests
.PHONY: test-integration
test-integration: test-cluster ## Run integration tests
	@echo "🔗 Running integration tests..."
	@kind export kubeconfig --name neo4j-operator-test && \
		go test ./test/integration/... -v -timeout=60m

.PHONY: test-integration-ci
test-integration-ci: ginkgo ## Run integration tests in CI (assumes cluster already exists)
	@echo "🔗 Running integration tests in CI with Ginkgo..."
	@if [ -z "$$KUBECONFIG" ]; then \
		echo "KUBECONFIG not set, trying to export from kind cluster..."; \
		export KUBECONFIG="$(HOME)/.kube/config"; \
		kind export kubeconfig --name neo4j-operator-test --kubeconfig="$$KUBECONFIG"; \
	fi
	@echo "Using KUBECONFIG: $$KUBECONFIG"
	@echo "📊 Running with enhanced progress output..."
	@KUBECONFIG="$$KUBECONFIG" $(GINKGO) run --timeout=60m --procs=1 -v ./test/integration/...

# E2E Tests - Removed to simplify test structure

# Test Suites
# Removed: test-no-cluster target - redundant alias for test-unit

.PHONY: test
test: test-unit test-integration ## Run all tests
	@echo "✅ All tests completed"

.PHONY: test-coverage
test-coverage: ## Generate coverage report
	@echo "📊 Generating coverage report..."
	@mkdir -p coverage
	@./scripts/run-tests-clean.sh go test -coverprofile=coverage/coverage.out -covermode=atomic ./...
	@go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@go tool cover -func=coverage/coverage.out | tail -1

.PHONY: test-ci-local
test-ci-local: ## Emulate CI workflow locally with debug logging (for troubleshooting)
	@echo "🔄 Emulating CI workflow locally with debug logging..."
	@echo "📁 Creating logs directory..."
	@mkdir -p logs
	@echo "🧹 Cleaning up previous test environment..."
	@$(MAKE) test-destroy || true
	@echo ""
	@echo "🧪 === PHASE 1: UNIT TESTS ==="
	@echo "⏰ Started at: $$(date)"
	@echo "Running unit tests with debug logging..." | tee logs/ci-local-unit.log
	@{ \
		echo "Environment variables:"; \
		echo "CI=true"; \
		echo "GITHUB_ACTIONS=true"; \
		echo "GO_VERSION=$$(go version)"; \
		echo "KUBECTL_VERSION=$$(kubectl version --client --short 2>/dev/null || echo 'kubectl not available')"; \
		echo ""; \
		CI=true GITHUB_ACTIONS=true $(MAKE) test-unit 2>&1; \
	} | tee -a logs/ci-local-unit.log
	@if [ $$? -ne 0 ]; then \
		echo "❌ Unit tests failed! Check logs/ci-local-unit.log for details"; \
		exit 1; \
	fi
	@echo "✅ Unit tests passed!"
	@echo ""
	@echo "🔗 === PHASE 2: INTEGRATION TESTS ==="
	@echo "⏰ Started at: $$(date)"
	@echo "Setting up test cluster..." | tee logs/ci-local-integration.log
	@{ \
		echo "Creating test cluster with CI-appropriate resources..."; \
		echo "Memory constraints: CI=true enables 512Mi memory limits"; \
		echo ""; \
		$(MAKE) test-cluster 2>&1; \
	} | tee -a logs/ci-local-integration.log
	@if [ $$? -ne 0 ]; then \
		echo "❌ Test cluster creation failed! Check logs/ci-local-integration.log for details"; \
		exit 1; \
	fi
	@echo "📦 Deploying operator to test cluster..." | tee -a logs/ci-local-integration.log
	@{ \
		echo "Deploying Neo4j operator..."; \
		$(MAKE) operator-setup 2>&1; \
	} | tee -a logs/ci-local-integration.log
	@if [ $$? -ne 0 ]; then \
		echo "❌ Operator deployment failed! Check logs/ci-local-integration.log for details"; \
		exit 1; \
	fi
	@echo "🧪 Running integration tests with CI environment variables..." | tee -a logs/ci-local-integration.log
	@{ \
		echo "Environment: CI=true GITHUB_ACTIONS=true"; \
		echo "Resource requirements: Using getCIAppropriateResourceRequirements()"; \
		echo "Memory limits: 512Mi (CI) vs 1.5Gi (local)"; \
		echo "Timeout: 60 minutes"; \
		echo ""; \
		CI=true GITHUB_ACTIONS=true kind export kubeconfig --name neo4j-operator-test 2>&1; \
		CI=true GITHUB_ACTIONS=true go test ./test/integration/... -v -timeout=60m 2>&1; \
	} | tee -a logs/ci-local-integration.log
	@if [ $$? -ne 0 ]; then \
		echo "❌ Integration tests failed! Check logs/ci-local-integration.log for details"; \
		echo "💡 Troubleshooting:"; \
		echo "   - Check operator logs: kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager"; \
		echo "   - Check pod status: kubectl get pods --all-namespaces"; \
		echo "   - Check events: kubectl get events --all-namespaces --sort-by='.lastTimestamp'"; \
		exit 1; \
	fi
	@echo "✅ Integration tests passed!"
	@echo ""
	@echo "🧹 === CLEANUP ==="
	@echo "⏰ Started at: $$(date)"
	@$(MAKE) test-destroy 2>&1 | tee logs/ci-local-cleanup.log
	@echo ""
	@echo "🎉 === CI WORKFLOW COMPLETE ==="
	@echo "⏰ Finished at: $$(date)"
	@echo "📋 Summary:"
	@echo "   ✅ Unit tests: PASSED"
	@echo "   ✅ Integration tests: PASSED"
	@echo "   ✅ Environment cleanup: COMPLETED"
	@echo ""
	@echo "📁 Debug logs available:"
	@echo "   - logs/ci-local-unit.log       - Unit test output"
	@echo "   - logs/ci-local-integration.log - Integration test output"
	@echo "   - logs/ci-local-cleanup.log    - Cleanup output"
	@echo ""
	@echo "💡 Use this target to reproduce CI issues locally with identical resource constraints"

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

# Removed: run target - operator must run in-cluster for proper DNS resolution

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=true -f -

# Removed: deploy/undeploy targets - use explicit deploy-prod or deploy-dev instead

.PHONY: deploy-dev
deploy-dev: manifests kustomize ## Deploy controller with development configuration to the K8s cluster.
	$(KUSTOMIZE) build config/overlays/dev | $(KUBECTL) apply -f -

.PHONY: deploy-prod
deploy-prod: manifests kustomize ## Deploy controller with production configuration to the K8s cluster.
	cd config/overlays/prod && $(KUSTOMIZE) edit set image controller=ghcr.io/neo4j-labs/neo4j-kubernetes-operator:$(VERSION)
	$(KUSTOMIZE) build config/overlays/prod | $(KUBECTL) apply -f -

.PHONY: undeploy-dev
undeploy-dev: kustomize ## Undeploy development controller from the K8s cluster.
	$(KUSTOMIZE) build config/overlays/dev | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: undeploy-prod
undeploy-prod: kustomize ## Undeploy production controller from the K8s cluster.
	$(KUSTOMIZE) build config/overlays/prod | $(KUBECTL) delete --ignore-not-found=true -f -

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
GINKGO = $(LOCALBIN)/ginkgo

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.3
CONTROLLER_TOOLS_VERSION ?= v0.16.1
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.64.8
GINKGO_VERSION ?= v2.23.4

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

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary.
$(GINKGO): $(LOCALBIN)
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo,$(GINKGO_VERSION))

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

# Removed: catalog-build-test and catalog-push-test targets - duplicates of catalog-build and catalog-push

##@ Development Environment

.PHONY: demo
demo: demo-setup ## Run interactive demo of the operator capabilities
	@echo "Starting Neo4j Kubernetes Operator demo..."
	@./scripts/demo.sh

.PHONY: demo-fast
demo-fast: demo-setup ## Run fast automated demo (no confirmations)
	@echo "Starting fast automated demo..."
	@./scripts/demo.sh --skip-confirmations --speed fast

.PHONY: demo-only
demo-only: ## Run fast demo without environment setup (assumes cluster exists)
	@echo "Running fast demo on existing environment..."
	@./scripts/demo.sh --skip-confirmations --speed fast

.PHONY: demo-interactive
demo-interactive: ## Run interactive demo without environment setup (assumes cluster exists)
	@echo "Running interactive demo on existing environment..."
	@./scripts/demo.sh

.PHONY: demo-setup
demo-setup: ## Setup complete demo environment (cluster + operator)
	@SKIP_SETUP_CONFIRMATION=true ./scripts/demo-setup.sh

.PHONY: dev-cluster
dev-cluster: ## Create a Kind cluster for development
	@echo "Creating development cluster..."
	@./scripts/setup-kind-dirs.sh
	@if ! kind get clusters | grep -q "neo4j-operator-dev"; then \
		kind create cluster --name neo4j-operator-dev --config hack/kind-config.yaml; \
		echo "Waiting for cluster to be ready..."; \
		kubectl wait --for=condition=ready node --all --timeout=300s; \
		echo "Installing cert-manager..."; \
		kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml; \
		kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s; \
		echo "Creating self-signed ClusterIssuer for development..."; \
		kubectl apply -f config/dev/self-signed-issuer.yaml || echo "Self-signed issuer creation skipped (file may not exist)"; \
		echo "Development cluster ready!"; \
	else \
		echo "Development cluster already exists"; \
	fi

.PHONY: test-cluster
test-cluster: ## Create a Kind cluster for testing
	@echo "Creating test cluster..."
	@./scripts/test-env.sh cluster

.PHONY: test-cluster-clean
test-cluster-clean: ## Clean operator resources from test cluster
	@echo "Cleaning operator resources from test cluster..."
	@if kind get clusters | grep -q "neo4j-operator-test"; then \
		echo "Switching to test cluster context..."; \
		kind export kubeconfig --name neo4j-operator-test; \
		echo "Removing operator deployment..."; \
		kubectl delete namespace neo4j-operator-system --ignore-not-found=true --timeout=60s; \
		echo "Removing test resources..."; \
		kubectl delete namespace neo4j --ignore-not-found=true --timeout=60s; \
		echo "Removing CRDs..."; \
		kubectl delete crd --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		echo "Removing cluster roles and bindings..."; \
		kubectl delete clusterrole --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		kubectl delete clusterrolebinding --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		echo "Test cluster cleaned!"; \
	else \
		echo "Test cluster not found, skipping cleanup."; \
	fi

.PHONY: test-cluster-delete
test-cluster-delete: ## Delete the test cluster
	@echo "Deleting test cluster..."
	@kind delete cluster --name neo4j-operator-test 2>/dev/null || true

.PHONY: test-cluster-reset
test-cluster-reset: test-cluster-delete test-cluster ## Reset test cluster (delete and recreate)
	@echo "Test cluster reset complete!"

.PHONY: test-destroy
test-destroy: ## Completely destroy test environment
	@echo "Destroying test environment..."
	@./scripts/test-env.sh cleanup
	@echo "Test environment destroyed!"

.PHONY: operator-setup
operator-setup: ## Set up the Neo4j operator (automated)
	@echo "🔧 Setting up Neo4j operator..."
	@SKIP_OPERATOR_CONFIRMATION=true ./scripts/setup-operator.sh setup

.PHONY: operator-setup-interactive
operator-setup-interactive: ## Set up the Neo4j operator interactively
	@echo "🔧 Setting up Neo4j operator (interactive mode)..."
	@./scripts/setup-operator.sh setup

.PHONY: operator-status
operator-status: ## Show operator status
	@echo "📊 Checking operator status..."
	@./scripts/setup-operator.sh status

.PHONY: operator-logs
operator-logs: ## Follow operator logs
	@echo "📋 Following operator logs..."
	@./scripts/setup-operator.sh logs

.PHONY: dev-cluster-clean
dev-cluster-clean: ## Clean operator resources from dev cluster
	@echo "Cleaning operator resources from development cluster..."
	@if kind get clusters | grep -q "neo4j-operator-dev"; then \
		echo "Switching to dev cluster context..."; \
		kind export kubeconfig --name neo4j-operator-dev; \
		echo "Removing operator deployment..."; \
		kubectl delete namespace neo4j-operator-system --ignore-not-found=true --timeout=120s; \
		echo "Removing CRDs..."; \
		kubectl delete crd --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		echo "Removing cluster roles and bindings..."; \
		kubectl delete clusterrole --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		kubectl delete clusterrolebinding --selector=app.kubernetes.io/name=neo4j-operator --ignore-not-found=true; \
		echo "Development cluster cleaned!"; \
	else \
		echo "Development cluster not found, skipping cleanup."; \
	fi

.PHONY: dev-cluster-delete
dev-cluster-delete: ## Delete the Kind development cluster
	@echo "Deleting development cluster..."
	@kind delete cluster --name neo4j-operator-dev || true

.PHONY: dev-cluster-reset
dev-cluster-reset: dev-cluster-delete dev-cluster ## Reset development cluster (delete and recreate)
	@echo "Development cluster reset complete!"

# Removed: dev-run target - operator must run in-cluster for proper DNS resolution

.PHONY: dev-cleanup
dev-cleanup: ## Clean up development environment completely
	@echo "Cleaning up development environment..."
	@hack/cleanup-dev.sh
	@if kind get clusters | grep -q "neo4j-operator-dev"; then \
		echo "Development cluster still exists. Use 'make dev-cluster-delete' to remove it."; \
	fi

.PHONY: dev-destroy
dev-destroy: ## Completely destroy development environment
	@echo "Destroying development environment..."
	@hack/cleanup-dev.sh || true
	@kind delete cluster --name neo4j-operator-dev || true
	@echo "Development environment destroyed!"

##@ Code Quality

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
	@$(MAKE) test-cleanup
