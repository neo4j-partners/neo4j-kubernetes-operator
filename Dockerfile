# Build the manager binary
FROM golang:1.22 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY src/ src/
COPY hack/ hack/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /manager ./src/cmd/manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
