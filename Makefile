# Image URL to use for all controller-related images
IMG ?= controller:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTROLLER_GEN defines the controller-gen binary.
CONTROLLER_GEN ?= $(shell go env GOPATH)/bin/controller-gen

# CONTROLLER_TOOLS_VERSION defines the controller-tools version for code generation
CONTROLLER_TOOLS_VERSION ?= v0.16.5

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: generate manifests build

##@ Development

.PHONY: run
run: ## Run the controller locally against the current kubeconfig
	go run ./src/cmd/manager/main.go --leader-elect=false

.PHONY: build
build: ## Build manager binary to bin/manager
	@mkdir -p bin
	go build -o bin/manager ./src/cmd/manager

.PHONY: docker-build
docker-build: ## Build the manager Docker image
	docker build -t ${IMG} .

.PHONY: test
test: generate ## Run all unit tests under src/
	go test ./src/...

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./src/...

.PHONY: vet
vet: ## Run go vet
	go vet ./src/...

.PHONY: lint-reconcile
lint-reconcile: ## Lint controller reconcile anti-patterns
	python3 .cursor/skills/kubernetes-operator/scripts/reconcile_lint.py --controller src/internal/controller/

.PHONY: audit
audit: manifests lint-reconcile validate-crd ## Run operator audit (CRD + reconcile lint)

##@ Deployment (kind / local cluster)

.PHONY: install
install: manifests ## Install CRDs into the cluster
	kubectl apply -f config/crd/bases/neo4j.com_neo4js.yaml

.PHONY: deploy
deploy: install ## Deploy operator to neo4j-operator-system
	kubectl apply -f config/default/namespace.yaml
	kubectl apply -k config/rbac
	kubectl apply -k config/manager

.PHONY: undeploy
undeploy: ## Remove operator deployment (keeps CRD and Neo4j workloads)
	kubectl delete -k config/manager --ignore-not-found
	kubectl delete -k config/rbac --ignore-not-found

.PHONY: sample-standalone
sample-standalone: install ## Apply Standalone sample (namespace graph-dev)
	kubectl create namespace graph-dev --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml

##@ Code generation

.PHONY: manifests
manifests: controller-gen ## Generate CRD and RBAC manifests
	$(CONTROLLER_GEN) rbac:roleName=neo4j-operator-manager-role paths="./src/internal/controller/..." output:rbac:artifacts:config=config/rbac
	$(CONTROLLER_GEN) crd paths="./src/api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate deepcopy code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./src/api/..."

.PHONY: controller-gen
controller-gen:
	@which controller-gen > /dev/null || go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: validate-crd
validate-crd: manifests ## Run CRD structural validator
	python3 .cursor/skills/kubernetes-operator/scripts/crd_validator.py --crd config/crd/
