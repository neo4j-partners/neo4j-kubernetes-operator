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
docker-build: ## Build the manager Docker image (set DOCKER_PLATFORM=linux/amd64 for CI)
	@if [ -n "$(DOCKER_PLATFORM)" ]; then \
		docker build --platform "$(DOCKER_PLATFORM)" -t "$(IMG)" .; \
	else \
		docker build -t "$(IMG)" .; \
	fi

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
	kubectl apply --server-side --force-conflicts -f config/crd/bases/neo4j.com_neo4js.yaml

.PHONY: deploy
deploy: install ## Deploy operator to neo4j-operator-system
	kubectl apply -f config/default/namespace.yaml
	# Drop ClusterRole leftovers from older installs (watch scope uses Role now).
	kubectl delete clusterrolebinding neo4j-operator-manager-rolebinding --ignore-not-found
	kubectl delete clusterrole neo4j-operator-manager-role --ignore-not-found
	kubectl apply -k config/rbac
	kubectl apply -k config/manager

.PHONY: undeploy
undeploy: ## Remove operator deployment (keeps CRD and Neo4j workloads)
	kubectl delete -k config/manager --ignore-not-found
	kubectl delete -k config/rbac --ignore-not-found

.PHONY: sample-standalone
sample-standalone: install ## Apply Standalone sample (default namespace)
	kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml

.PHONY: test-e2e
test-e2e: ## Run e2e suite (CLOUD, E2E_PROFILE=happy-path|matrix|explicit, SUITE=)
	chmod +x tests/bin/*.sh tests/runner/*.sh tests/actions/*/*/*.sh tests/config/**/*.sh tests/lib/*.sh 2>/dev/null || true
	CLOUD=$${CLOUD:-local-kind} E2E_PROFILE=$${E2E_PROFILE:-happy-path} ./tests/bin/run-e2e.sh $${SUITE:-$${SCENARIO:-p0-standalone}}

.PHONY: test-e2e-matrix
test-e2e-matrix: ## Run all reconciled e2e combinations on local-kind
	bash tests/bin/setup-local-kind.sh
	$(MAKE) test-e2e CLOUD=local-kind E2E_PROFILE=matrix

.PHONY: test-e2e-combinations
test-e2e-combinations: ## List e2e matrix combinations (CLOUD, SCENARIO)
	chmod +x tests/bin/list-e2e-combinations.sh 2>/dev/null || true
	CLOUD=$${CLOUD:-local-kind} SCENARIO=$${SCENARIO:-p0-standalone} ./tests/bin/list-e2e-combinations.sh

.PHONY: test-e2e-admission
test-e2e-admission: ## Run neo4j-admission suite on local-kind (shared operator setup)
	bash tests/bin/setup-local-kind.sh
	CLOUD=local-kind E2E_PROFILE=happy-path ./tests/bin/run-e2e.sh neo4j-admission

.PHONY: test-e2e-local
test-e2e-local: ## Prepare kind + run e2e on local-kind
	bash tests/bin/setup-local-kind.sh
	$(MAKE) test-e2e CLOUD=local-kind

.PHONY: test-e2e-azure
test-e2e-azure: ## Ensure AKS, push image, run e2e on azure-aks
	bash -c 'source tests/azure/ensure-aks.sh && bash tests/azure/push-operator-image.sh && CLOUD=azure-aks ./tests/bin/run-e2e.sh'

.PHONY: test-e2e-azure-matrix
test-e2e-azure-matrix: ## Ensure AKS, push image, run all e2e matrix combinations on azure-aks
	bash -c 'source tests/azure/ensure-aks.sh && bash tests/azure/push-operator-image.sh && CLOUD=azure-aks E2E_PROFILE=matrix ./tests/bin/run-e2e.sh'

##@ Code generation

.PHONY: manifests
manifests: controller-gen ## Generate CRD manifests
	# Manager RBAC is hand-maintained Role/RoleBinding per WATCH_NAMESPACE (config/rbac/role.yaml).
	# Do not run controller-gen rbac — it emits a ClusterRole and overwrites that file.
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
