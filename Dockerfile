# Build the manager binary.
#
# The builder is pinned to the *build* platform and cross-compiles, rather than running under
# emulation for each target: the manager is pure Go with cgo off, so GOARCH alone decides the
# output and QEMU would only buy a tenfold slower build. TARGETOS/TARGETARCH are supplied by
# buildx per platform in the release matrix; both default so a plain `docker build` still works.
# Pinned by manifest-list digest (NEO-008): the tag is a human hint, the digest is authoritative and
# keeps multi-arch buildx working. Bump tag+digest together (Renovate/Dependabot digest-pinning).
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY src/ src/
COPY hack/ hack/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -o /manager ./src/cmd/manager

# Pinned by manifest-list digest (NEO-008); bump tag+digest together.
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=builder /manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
