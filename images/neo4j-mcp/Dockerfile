# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder

ARG MCP_REPO=https://github.com/neo4j/mcp.git
ARG MCP_REF=v1.1.1
ARG MCP_VERSION=v1.1.1

RUN apk add --no-cache ca-certificates git

WORKDIR /build
RUN git clone --depth 1 --branch "${MCP_REF}" "${MCP_REPO}" .

RUN CGO_ENABLED=0 GOOS=linux go build -C cmd/neo4j-mcp -a -installsuffix cgo \
    -ldflags "-X main.Version=${MCP_VERSION}" \
    -o /out/neo4j-mcp

FROM scratch

WORKDIR /app

COPY --from=builder /out/neo4j-mcp /app/neo4j-mcp
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 65532:65532

ENTRYPOINT ["/app/neo4j-mcp"]
