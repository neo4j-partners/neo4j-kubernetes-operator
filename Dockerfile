# Build the manager binary - Neo4j Enterprise Operator
FROM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies (cached across builds via BuildKit)
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build with enterprise-only support
# BuildKit cache mounts dramatically speed up repeated builds (~40s -> ~5s)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -a -installsuffix cgo \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.vcsRef=${VCS_REF}" \
    -o manager cmd/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

# Add documentation (if files exist)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Re-declare the build args in this stage so the image-metadata LABELs below
# actually interpolate them — ARGs declared before the first FROM do not cross
# stage boundaries. Without this the version/created/revision labels are empty
# and buildx emits an UndefinedVar warning.
ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

# Add metadata
LABEL org.opencontainers.image.title="Neo4j Enterprise Operator"
LABEL org.opencontainers.image.description="Kubernetes operator for Neo4j Enterprise clusters (5.26+)"
LABEL org.opencontainers.image.vendor="Neo4j Labs"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.source="https://github.com/neo4j-partners/neo4j-kubernetes-operator"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.documentation="https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/README.md"

# Enterprise-only metadata
LABEL neo4j.edition="enterprise-only"
LABEL neo4j.min-version="5.26"
LABEL neo4j.community-edition="unsupported"
LABEL neo4j.description="This operator only supports Neo4j Enterprise Edition 5.26 and above"

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
