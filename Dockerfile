FROM golang:1.21 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum .
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY internal/ internal/

# Build
RUN mkdir build && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o build ./cmd/...

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.19
#FROM gcr.io/distroless/static:nonroot
#WORKDIR /
ENV PATH="/opt/turnip/bin:${PATH}"
#RUN mkdir -p /opt/turnip/bin && chown -R 65532:65532 /opt/turnip && chmod -R 755 /opt/turnip
#USER 65532:65532

COPY --from=builder /workspace/build/ /opt/turnip/bin/
