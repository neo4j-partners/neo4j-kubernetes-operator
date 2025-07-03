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
test-setup: ## Setup test environment (cleanup + validation)
	@echo "🔧 Setting up test environment..."
	@if [ -f "scripts/test-environment-manager.sh" ]; then \
		chmod +x scripts/test-environment-manager.sh; \
		./scripts/test-environment-manager.sh setup; \
	else \
		echo "❌ Test environment manager not found"; \
		exit 1; \
	fi

.PHONY: test-check
test-check: ## Check test environment requirements
	@echo "🔍 Checking test environment..."
	@if [ -f "scripts/test-environment-manager.sh" ]; then \
		chmod +x scripts/test-environment-manager.sh; \
		./scripts/test-environment-manager.sh check; \
	else \
		echo "❌ Test environment manager not found"; \
		exit 1; \
	fi

.PHONY: test-cleanup
test-cleanup: ## Clean up test environment and artifacts
	@echo "🧹 Cleaning up test environment..."
	@if [ -f "scripts/test-environment-manager.sh" ]; then \
		chmod +x scripts/test-environment-manager.sh; \
		./scripts/test-environment-manager.sh cleanup; \
	else \
		echo "⚠️  Test environment manager not found, using fallback cleanup"; \
		rm -rf test-results coverage logs tmp; \
		rm -f test-output.log coverage-*.out coverage-*.html; \
	fi

.PHONY: test-env-detect
test-env-detect: ## Detect and display current test environment
	@echo "🔍 Detecting test environment..."
	@if [ -f "scripts/test-environment-manager.sh" ]; then \
		chmod +x scripts/test-environment-manager.sh; \
		./scripts/test-environment-manager.sh detect; \
	else \
		echo "❌ Test environment manager not found"; \
		exit 1; \
	fi

# Unit Tests
.PHONY: test-unit
test-unit: manifests generate fmt vet envtest ## Run unit tests (no cluster required).
	@echo "🧪 Running unit tests..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh unit --no-setup; \
	else \
		echo "📋 Running unit tests with go test..."; \
		KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e | grep -v /integration | grep -v "/test/webhooks") -coverprofile coverage/coverage-unit.out -race -v; \
	fi

.PHONY: test-webhooks
test-webhooks: manifests generate fmt vet envtest ## Run webhook tests (no cluster required).
	@echo "🔗 Running webhook tests..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh webhooks --no-setup; \
	else \
		echo "📋 Running webhook tests with go test..."; \
		KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./internal/webhooks/... ./test/webhooks/... -v -timeout=10m; \
	fi

.PHONY: test-webhooks-tls
test-webhooks-tls: ## Test webhook TLS configuration in development cluster
	@echo "🔐 Testing webhook TLS configuration..."
	@./hack/test-webhooks.sh

.PHONY: test-webhooks-integration
test-webhooks-integration: dev-cluster operator-setup ## Run webhook integration tests with TLS
	@echo "🔗 Running webhook integration tests with TLS..."
	@echo "Testing valid resource acceptance..."
	@kubectl apply -f config/samples/neo4j_v1alpha1_neo4jenterprisecluster.yaml --dry-run=server > /dev/null && echo "✅ Valid resource accepted" || echo "❌ Valid resource rejected"
	@echo "Testing invalid resource rejection..."
	@! kubectl apply -f test/fixtures/invalid-cluster.yaml --dry-run=server > /dev/null 2>&1 && echo "✅ Invalid resource rejected" || echo "❌ Invalid resource accepted"
	@echo "Testing webhook certificate..."
	@kubectl get secret webhook-server-cert -n neo4j-operator-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -q "Subject:" && echo "✅ Webhook certificate valid" || echo "❌ Webhook certificate invalid"

.PHONY: test-webhook-cert-rotation
test-webhook-cert-rotation: dev-cluster operator-setup ## Test webhook certificate rotation
	@echo "🔄 Testing webhook certificate rotation..."
	@kubectl delete secret webhook-server-cert -n neo4j-operator-system || true
	@echo "Waiting for certificate regeneration..."
	@kubectl wait --for=condition=ready certificate serving-cert -n neo4j-operator-system --timeout=120s
	@echo "Testing webhook functionality after rotation..."
	@kubectl apply -f config/samples/neo4j_v1alpha1_neo4jenterprisecluster.yaml --dry-run=server > /dev/null && echo "✅ Webhook working after cert rotation" || echo "❌ Webhook failed after cert rotation"

.PHONY: test-security
test-security: ## Run security-focused tests (no cluster required).
	@echo "🔒 Running security tests..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh unit --no-setup; \
	else \
		echo "📋 Running security tests with go test..."; \
		go test ./internal/controller/security_coordinator_test.go -v; \
		go test ./internal/webhooks/... -v -run=".*Security.*"; \
	fi

.PHONY: test-neo4j-client
test-neo4j-client: ## Run Neo4j client tests (no cluster required).
	@echo "🗄️  Running Neo4j client tests..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh unit --no-setup; \
	else \
		echo "📋 Running Neo4j client tests with go test..."; \
		go test ./internal/neo4j/... -v; \
	fi

.PHONY: test-controllers
test-controllers: ## Run controller tests (no cluster required).
	@echo "🎮 Running controller tests..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh unit --no-setup; \
	else \
		echo "📋 Running controller tests with go test..."; \
		go test ./internal/controller/... -v -run="Test.*" -timeout=10m; \
	fi

# Integration Tests
.PHONY: test-integration

test-integration: ## Run integration tests with webhooks and cert-manager
	@echo "🔗 Running integration tests with webhooks and cert-manager..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh integration; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh integration; \
	else \
		echo "📋 Running integration tests with legacy script..."; \
		@./scripts/run-tests.sh integration; \
	fi

# E2E Tests
.PHONY: test-e2e
test-e2e: test-setup ## Run e2e tests with webhooks and cert-manager (requires cluster).
	@echo "🌐 Running e2e tests with webhooks and cert-manager..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh e2e; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh e2e --no-setup; \
	else \
		echo "📋 Running e2e tests with ginkgo..."; \
		export E2E_TEST=true; export KIND_CLUSTER=neo4j-operator-test; go test -v ./test/e2e/... -ginkgo.v -ginkgo.timeout=1h; \
	fi

# Smoke Tests
.PHONY: test-smoke
test-smoke: ## Run smoke tests (basic functionality)
	@echo "💨 Running smoke tests..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh smoke; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh smoke; \
	else \
		echo "❌ Smoke test script not found"; \
		exit 1; \
	fi

# Comprehensive Test Suites
.PHONY: test-no-cluster
test-no-cluster: test-unit test-webhooks test-security test-neo4j-client test-controllers ## Run all tests that don't require a cluster.

.PHONY: test-comprehensive
test-comprehensive: test-unit test-integration test-webhooks test-security ## Run comprehensive test suite with webhooks.

.PHONY: test-ci
test-ci: test-unit test-webhooks test-security test-integration ## Run CI test suite with webhooks.

# Unified Test Runner
.PHONY: test
test: ## Run all tests using unified test runner
	@echo "🚀 Running comprehensive test suite with webhooks..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh all --coverage; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh all --coverage; \
	else \
		echo "❌ Unified test runner not found, falling back to individual tests"; \
		$(MAKE) test-comprehensive; \
	fi

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	@echo "🚀 Running comprehensive test suite with verbose output..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh all --coverage --verbose; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh all --coverage --verbose; \
	else \
		echo "❌ Unified test runner not found"; \
		exit 1; \
	fi

.PHONY: test-fast
test-fast: ## Run fast test suite (unit + smoke)
	@echo "⚡ Running fast test suite..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh unit --coverage; \
		./scripts/test-with-operator.sh smoke --no-setup; \
	elif [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh unit --coverage; \
		./scripts/run-tests.sh smoke --no-setup; \
	else \
		echo "❌ Unified test runner not found"; \
		exit 1; \
	fi

# Test with Operator Setup
.PHONY: test-with-operator
test-with-operator: ## Run tests with automatic operator setup
	@echo "🔧 Running tests with automatic operator setup..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh all --coverage --verbose; \
	else \
		echo "❌ Test with operator script not found"; \
		exit 1; \
	fi

.PHONY: test-with-operator-unit
test-with-operator-unit: ## Run unit tests with operator setup script
	@echo "🧪 Running unit tests with operator setup script..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh unit --coverage; \
	else \
		echo "❌ Test with operator script not found"; \
		exit 1; \
	fi

.PHONY: test-with-operator-integration
test-with-operator-integration: ## Run integration tests with operator setup script
	@echo "🔗 Running integration tests with operator setup script..."
	@if [ -f "scripts/test-with-operator.sh" ]; then \
		chmod +x scripts/test-with-operator.sh; \
		./scripts/test-with-operator.sh integration --coverage; \
	else \
		echo "❌ Test with operator script not found"; \
		exit 1; \
	fi

# Test Coverage
.PHONY: test-coverage
test-coverage: ## Generate comprehensive coverage report
	@echo "📊 Generating coverage report..."
	@if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh all --coverage --verbose; \
	else \
		echo "📋 Generating coverage with go test..."; \
		go test -coverprofile=coverage/coverage.out -covermode=atomic ./...; \
		go tool cover -html=coverage/coverage.out -o coverage/coverage.html; \
		go tool cover -func=coverage/coverage.out | tail -1; \
	fi

# Test Debug
.PHONY: test-debug
test-debug: ## Run tests in debug mode
	@echo "🐛 Running tests in debug mode..."
	@export TEST_DEBUG=true; \
	if [ -f "scripts/run-tests.sh" ]; then \
		chmod +x scripts/run-tests.sh; \
		./scripts/run-tests.sh all --verbose --retain-logs; \
	else \
		echo "❌ Unified test runner not found"; \
		exit 1; \
	fi

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

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

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: deploy-test-with-webhooks
deploy-test-with-webhooks: manifests kustomize ## Deploy controller to the K8s cluster with webhooks enabled and cert-manager for testing.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/test-with-webhooks | $(KUBECTL) apply -f -

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

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.3
CONTROLLER_TOOLS_VERSION ?= v0.16.1
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.64.8

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

# Build catalog image which contains a list of bundle images for testing, then push the image.
.PHONY: catalog-build-test
catalog-build-test: opm ## Build a catalog image for testing.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image for testing.
.PHONY: catalog-push-test
catalog-push-test: ## Push a catalog image for testing.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ Development

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

.PHONY: operator-setup
operator-setup: ## Set up the Neo4j operator with webhooks and cert-manager for development/testing.
	@echo "🔧 Setting up Neo4j operator with webhooks and cert-manager..."
	@if [ -f "scripts/setup-operator.sh" ]; then \
		chmod +x scripts/setup-operator.sh; \
		./scripts/setup-operator.sh setup; \
	else \
		echo "❌ Operator setup script not found"; \
		exit 1; \
	fi

.PHONY: operator-status
operator-status: ## Show operator status and configuration.
	@echo "📊 Checking operator status..."
	@if [ -f "scripts/setup-operator.sh" ]; then \
		chmod +x scripts/setup-operator.sh; \
		./scripts/setup-operator.sh status; \
	else \
		echo "❌ Operator setup script not found"; \
		exit 1; \
	fi

.PHONY: operator-logs
operator-logs: ## Follow operator logs.
	@echo "📋 Following operator logs..."
	@if [ -f "scripts/setup-operator.sh" ]; then \
		chmod +x scripts/setup-operator.sh; \
		./scripts/setup-operator.sh logs; \
	else \
		echo "❌ Operator setup script not found"; \
		exit 1; \
	fi

.PHONY: operator-cleanup
operator-cleanup: ## Clean up operator setup artifacts.
	@echo "🧹 Cleaning up operator setup..."
	@if [ -f "scripts/setup-operator.sh" ]; then \
		chmod +x scripts/setup-operator.sh; \
		./scripts/setup-operator.sh cleanup; \
	else \
		echo "❌ Operator setup script not found"; \
		exit 1; \
	fi

.PHONY: dev-cluster-delete
dev-cluster-delete: ## Delete the Kind development cluster.
	@echo "Deleting development cluster..."
	@kind delete cluster --name neo4j-operator-dev || true

.PHONY: dev-run
dev-run: ## Run the operator locally for development.
	@hack/dev-run.sh

.PHONY: dev-cleanup
dev-cleanup: ## Clean up development environment.
	@hack/cleanup-dev.sh

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
