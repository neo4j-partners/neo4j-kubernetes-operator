# Neo4j kubectl Plugin Makefile

# Variables
PLUGIN_NAME := kubectl-neo4j
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build variables
GO_VERSION := 1.21
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

# Build flags
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

# Directories
BUILD_DIR := bin
DIST_DIR := dist

# Targets
.PHONY: all build clean test lint install uninstall help

all: build ## Build the plugin

build: ## Build the kubectl plugin
	@echo "Building $(PLUGIN_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(PLUGIN_NAME) \
		./main.go

build-all: ## Build for all supported platforms
	@echo "Building for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			if [ "$$os" = "windows" ] && [ "$$arch" = "arm64" ]; then continue; fi; \
			echo "Building for $$os/$$arch..."; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build \
				-ldflags "$(LDFLAGS)" \
				-o $(DIST_DIR)/$(PLUGIN_NAME)-$$os-$$arch \
				./main.go; \
			if [ "$$os" = "windows" ]; then \
				mv $(DIST_DIR)/$(PLUGIN_NAME)-$$os-$$arch $(DIST_DIR)/$(PLUGIN_NAME)-$$os-$$arch.exe; \
			fi; \
		done; \
	done

test: ## Run tests
	@echo "Running tests..."
	go test -v ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	@echo "Running linter..."
	golangci-lint run

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

mod-tidy: ## Tidy go modules
	@echo "Tidying go modules..."
	go mod tidy

install: build ## Install the plugin locally
	@echo "Installing $(PLUGIN_NAME)..."
	@if [ ! -d "$$HOME/.local/bin" ]; then mkdir -p "$$HOME/.local/bin"; fi
	@cp $(BUILD_DIR)/$(PLUGIN_NAME) $$HOME/.local/bin/
	@chmod +x $$HOME/.local/bin/$(PLUGIN_NAME)
	@echo "$(PLUGIN_NAME) installed to $$HOME/.local/bin/"
	@echo "Make sure $$HOME/.local/bin is in your PATH"

install-system: build ## Install the plugin system-wide
	@echo "Installing $(PLUGIN_NAME) system-wide..."
	@sudo cp $(BUILD_DIR)/$(PLUGIN_NAME) /usr/local/bin/
	@sudo chmod +x /usr/local/bin/$(PLUGIN_NAME)
	@echo "$(PLUGIN_NAME) installed to /usr/local/bin/"

uninstall: ## Uninstall the plugin
	@echo "Uninstalling $(PLUGIN_NAME)..."
	@rm -f $$HOME/.local/bin/$(PLUGIN_NAME)
	@sudo rm -f /usr/local/bin/$(PLUGIN_NAME)
	@echo "$(PLUGIN_NAME) uninstalled"

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@rm -f coverage.out coverage.html

verify: fmt vet lint test ## Run all verification steps

release: clean verify build-all ## Create a release build

docker-build: ## Build Docker image for development
	@echo "Building Docker image..."
	docker build -t $(PLUGIN_NAME):$(VERSION) .

docker-run: docker-build ## Run the plugin in Docker
	@echo "Running $(PLUGIN_NAME) in Docker..."
	docker run --rm -it $(PLUGIN_NAME):$(VERSION)

deps: ## Install development dependencies
	@echo "Installing development dependencies..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

version: ## Show version information
	@echo "Plugin: $(PLUGIN_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Go Version: $(GO_VERSION)"

help: ## Show this help message
	@echo "Neo4j kubectl Plugin"
	@echo "==================="
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Default target
.DEFAULT_GOAL := help
