# -*- mode: Python -*-

# Configuration
IMG_REPO = "neo4j-operator"
IMG_TAG = "dev"
IMG = IMG_REPO + ":" + IMG_TAG

# Load environment variables
load('ext://dotenv', 'dotenv')
dotenv()

# Load helpers
load('ext://restart_process', 'docker_build_with_restart')
load('ext://helm_remote', 'helm_remote')

# Configuration from environment
k8s_namespace = os.getenv('TILT_NAMESPACE', 'default')
debug_mode = os.getenv('TILT_DEBUG', 'false').lower() == 'true'
enable_webhooks = os.getenv('TILT_WEBHOOKS', 'false').lower() == 'true'

print("🚀 Neo4j Operator Development Environment")
print("   Namespace: %s" % k8s_namespace)
print("   Debug Mode: %s" % debug_mode)
print("   Webhooks: %s" % enable_webhooks)

# Set namespace
k8s_namespace(k8s_namespace)

# Install cert-manager if webhooks are enabled
if enable_webhooks:
    print("📜 Installing cert-manager...")
    helm_remote('cert-manager',
                repo_name='jetstack',
                repo_url='https://charts.jetstack.io',
                namespace='cert-manager',
                create_namespace=True,
                set=['installCRDs=true', 'global.leaderElection.namespace=cert-manager'])

# Build the operator image
print("🔨 Building operator image...")

# Create Dockerfile for development
dockerfile_dev = '''
FROM golang:1.21-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
'''

# Build image with live update
docker_build_with_restart(
    IMG,
    context='.',
    dockerfile_contents=dockerfile_dev,
    only=[
        './cmd',
        './api',
        './internal',
        './go.mod',
        './go.sum',
    ],
    live_update=[
        sync('./cmd', '/workspace/cmd'),
        sync('./api', '/workspace/api'),
        sync('./internal', '/workspace/internal'),
        sync('./go.mod', '/workspace/go.mod'),
        sync('./go.sum', '/workspace/go.sum'),
        run('cd /workspace && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o manager cmd/main.go', trigger=['./cmd', './api', './internal']),
    ],
    entrypoint=['/workspace/manager'],
)

# Install CRDs
print("📋 Installing CRDs...")
k8s_yaml(kustomize('./config/crd'))

# Create RBAC
print("🔐 Installing RBAC...")
k8s_yaml(kustomize('./config/rbac'))

# Create manager deployment
print("🎯 Creating manager deployment...")

# Base manager configuration
manager_yaml = '''
apiVersion: apps/v1
kind: Deployment
metadata:
  name: neo4j-operator-controller-manager
  namespace: {namespace}
  labels:
    app.kubernetes.io/name: neo4j-operator
    app.kubernetes.io/component: manager
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: neo4j-operator
      app.kubernetes.io/component: manager
  template:
    metadata:
      labels:
        app.kubernetes.io/name: neo4j-operator
        app.kubernetes.io/component: manager
    spec:
      serviceAccountName: neo4j-operator-controller-manager
      containers:
      - name: manager
        image: {image}
        imagePullPolicy: IfNotPresent
        args:
        - --leader-elect=false
        - --zap-devel=true
        - --zap-log-level=debug
        - --metrics-bind-address=:8080
        - --health-probe-bind-address=:8081
        ports:
        - containerPort: 8080
          name: metrics
          protocol: TCP
        - containerPort: 8081
          name: health
          protocol: TCP
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          limits:
            cpu: 500m
            memory: 512Mi
          requests:
            cpu: 100m
            memory: 64Mi
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          capabilities:
            drop:
            - ALL
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
'''.format(namespace=k8s_namespace, image=IMG)

# Add debug configuration if enabled
if debug_mode:
    print("🐛 Enabling debug mode...")
    manager_yaml = manager_yaml.replace(
        'args:\n        - --leader-elect=false',
        '''args:
        - --leader-elect=false
        - --pprof-bind-address=:6060'''
    )
    manager_yaml = manager_yaml.replace(
        'ports:\n        - containerPort: 8080',
        '''ports:
        - containerPort: 6060
          name: pprof
          protocol: TCP
        - containerPort: 8080'''
    )

# Add webhook configuration if enabled
if enable_webhooks:
    print("🎣 Enabling webhooks...")
    manager_yaml = manager_yaml.replace(
        '- --health-probe-bind-address=:8081',
        '''- --health-probe-bind-address=:8081
        - --webhook-port=9443
        - --webhook-cert-dir=/tmp/k8s-webhook-server/serving-certs'''
    )
    manager_yaml = manager_yaml.replace(
        'name: health\n          protocol: TCP',
        '''name: health
          protocol: TCP
        - containerPort: 9443
          name: webhook
          protocol: TCP'''
    )

k8s_yaml(blob(manager_yaml))

# Create service for metrics
metrics_service = '''
apiVersion: v1
kind: Service
metadata:
  name: neo4j-operator-metrics
  namespace: {namespace}
  labels:
    app.kubernetes.io/name: neo4j-operator
    app.kubernetes.io/component: metrics
spec:
  ports:
  - name: metrics
    port: 8080
    targetPort: 8080
  selector:
    app.kubernetes.io/name: neo4j-operator
    app.kubernetes.io/component: manager
'''.format(namespace=k8s_namespace)

k8s_yaml(blob(metrics_service))

# Port forwards
print("🌐 Setting up port forwards...")
k8s_resource('neo4j-operator-controller-manager',
             port_forwards=[
                 '8080:8080',  # Metrics
                 '8081:8081',  # Health
             ])

if debug_mode:
    k8s_resource('neo4j-operator-controller-manager',
                 port_forwards=[
                     '6060:6060',  # pprof
                 ])

# Load sample configurations
print("📝 Loading sample configurations...")

# Basic Neo4j cluster sample
basic_cluster_yaml = '''
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-auth
  namespace: {namespace}
type: Opaque
data:
  password: bmVvNGpwYXNzd29yZA==  # neo4jpassword
---
apiVersion: neo4j.com/v1alpha1
kind: Neo4jCluster
metadata:
  name: basic-neo4j
  namespace: {namespace}
spec:
  image: "neo4j:5.15.0-enterprise"
  edition: enterprise
  acceptLicenseAgreement: "yes"
  topology:
    primaryCount: 1
    secondaryCount: 0
  storage:
    size: "1Gi"
    storageClass: "standard"
  auth:
    password:
      secretName: "neo4j-auth"
      secretKey: "password"
  service:
    type: ClusterIP
'''.format(namespace=k8s_namespace)

k8s_yaml(blob(basic_cluster_yaml))

# Resource grouping
k8s_resource('neo4j-operator-controller-manager',
             labels=['operator'])
k8s_resource('neo4j-operator-metrics',
             labels=['operator'])
k8s_resource('basic-neo4j',
             labels=['samples'])

# Custom buttons
print("🎮 Setting up custom buttons...")

# Button to run tests
local_resource(
    'test-unit',
    cmd='make test-unit',
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['testing']
)

local_resource(
    'test-samples',
    cmd='make test-samples',
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['testing']
)

# Button to clean up
local_resource(
    'cleanup',
    cmd='make dev-cleanup',
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['cleanup']
)

# Button to view logs
local_resource(
    'logs',
    cmd='kubectl logs -f deployment/neo4j-operator-controller-manager -n ' + k8s_namespace,
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
    labels=['debugging']
)

print("✅ Tilt configuration loaded successfully!")
print("🔗 Access the Tilt UI at: http://localhost:10350")
print("📊 Metrics available at: http://localhost:8080/metrics")
print("🩺 Health check at: http://localhost:8081/healthz")

if debug_mode:
    print("🐛 pprof available at: http://localhost:6060/debug/pprof/")

print("\n🎯 Quick commands:")
print("   tilt up                    # Start development environment")
print("   tilt down                  # Stop development environment")
print("   tilt trigger test-unit     # Run unit tests")
print("   tilt trigger test-samples  # Test sample configurations")
print("   tilt trigger logs          # View operator logs")
