# Build the manager binary - Neo4j Enterprise Operator
FROM golang:1.23-alpine AS builder
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

# Download dependencies
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build with enterprise-only support
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -a -installsuffix cgo \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.vcsRef=${VCS_REF}" \
    -o manager cmd/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

# Add documentation (if files exist)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Add metadata
LABEL org.opencontainers.image.title="Neo4j Enterprise Operator"
LABEL org.opencontainers.image.description="Kubernetes operator for Neo4j Enterprise clusters (5.26+)"
LABEL org.opencontainers.image.vendor="Neo4j Labs"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.source="https://github.com/neo4j-labs/neo4j-kubernetes-operator"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.documentation="https://github.com/neo4j-labs/neo4j-kubernetes-operator/blob/main/README.md"

# Enterprise-only metadata
LABEL neo4j.edition="enterprise-only"
LABEL neo4j.min-version="5.26"
LABEL neo4j.community-edition="unsupported"
LABEL neo4j.description="This operator only supports Neo4j Enterprise Edition 5.26 and above"

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
