# Build the manager binary.
#
# The builder is pinned to the *build* platform and cross-compiles, rather than running under
# emulation for each target: the manager is pure Go with cgo off, so GOARCH alone decides the
# output and QEMU would only buy a tenfold slower build. TARGETOS/TARGETARCH are supplied by
# buildx per platform in the release matrix; both default so a plain `docker build` still works.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY src/ src/
COPY hack/ hack/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -o /manager ./src/cmd/manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
