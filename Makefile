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

# CONTROLLER_TOOLS_VERSION defines the controller-tools version for code generation
CONTROLLER_TOOLS_VERSION ?= v0.16.5

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: generate manifests

##@ Code generation

# CONTROLLER_GEN defines the controller-gen binary.
CONTROLLER_GEN ?= $(shell go env GOPATH)/bin/controller-gen

.PHONY: manifests
manifests: controller-gen ## Generate CRD manifests into config/crd/bases
	$(CONTROLLER_GEN) crd paths="./src/api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate deepcopy code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./src/api/..."

.PHONY: controller-gen
controller-gen:
	@which controller-gen > /dev/null || go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: test
test: generate ## Run unit tests under src/
	go test ./src/api/...

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: validate-crd
validate-crd: manifests ## Run CRD structural validator
	python3 .cursor/skills/kubernetes-operator/scripts/crd_validator.py --crd config/crd/
