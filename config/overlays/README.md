# Environment-Specific Deployment Overlays

This directory contains Kustomize overlays for different deployment environments, following Kubernetes best practices for configuration management.

## Structure

```
config/
├── overlays/
│   ├── dev/           # Development environment
│   └── prod/          # Production environment
├── default/           # Base configuration
└── manager/           # Core deployment manifests
```

## Usage

### Development Deployment
```bash
# Deploy to development environment
make deploy-dev

# Undeploy from development
make undeploy-dev

# Preview configuration
./bin/kustomize build config/overlays/dev
```

### Production Deployment
```bash
# Deploy to production environment (uses VERSION from Makefile)
make deploy-prod

# Deploy specific version
make deploy-prod VERSION=v0.0.3

# Undeploy from production
make undeploy-prod

# Preview configuration
./bin/kustomize build config/overlays/prod
```

## Environment Differences

### Development Environment (`config/overlays/dev/`)

**Image Configuration:**
- Image: `neo4j-operator:dev`
- Built locally with `make docker-build`

**Environment Variables:**
- `DEVELOPMENT_MODE=true`

**Namespace:**
- `neo4j-operator-dev`

**Resource Configuration:**
- Uses base resource limits (development-friendly)

### Production Environment (`config/overlays/prod/`)

**Image Configuration:**
- Image: `ghcr.io/neo4j-labs/neo4j-kubernetes-operator:$(VERSION)`
- Version determined by Makefile VERSION variable or command line
- Published production image from GitHub Container Registry

**Production Configuration:**
- Single replica (leader election provides HA)
- Resource requests/limits optimized for production

**Namespace:**
- `neo4j-operator-system`

**Resource Configuration:**
- CPU: 100m requests, 500m limits
- Memory: 64Mi requests, 128Mi limits

## Benefits

1. **Environment Separation**: Clear separation between dev and prod configurations
2. **No Manual Edits**: No need to manually modify kustomization.yaml files
3. **Version Control**: All environment configurations tracked in Git
4. **CI/CD Ready**: Easily integrated with deployment pipelines
5. **Safety**: Prevents accidental production deployments with dev images

## Migration from Legacy Approach

**Old Way (Error-Prone):**
```bash
# Manually edit config/manager/kustomization.yaml
vim config/manager/kustomization.yaml
make deploy
```

**New Way (Environment-Safe):**
```bash
# Environment-specific deployment
make deploy-dev    # or make deploy-prod
```

## Customization

### Adding New Environments

1. Create new directory: `config/overlays/staging/`
2. Create `kustomization.yaml` with environment-specific settings
3. Add Makefile targets for deployment

### Overriding Configuration

Each overlay can include:
- Image overrides
- Environment variables
- Resource limits
- Namespace settings
- Additional patches

Example custom patch:
```yaml
patches:
- patch: |-
    - op: add
      path: /spec/template/spec/containers/0/env/-
      value:
        name: CUSTOM_SETTING
        value: "custom-value"
  target:
    kind: Deployment
    name: controller-manager
```

## Troubleshooting

### Common Issues

1. **Wrong deployment name in patches**: Use `controller-manager`, not `neo4j-operator-controller-manager`
2. **Missing base configuration**: Ensure `resources: - ../../default` is present
3. **Image pull issues**: Verify image names and tags are correct

### Validation

```bash
# Validate overlay syntax
./bin/kustomize build config/overlays/dev > /dev/null
./bin/kustomize build config/overlays/prod > /dev/null

# Check image references
./bin/kustomize build config/overlays/dev | grep "image:"
./bin/kustomize build config/overlays/prod | grep "image:"
```
