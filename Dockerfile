# Build on the runner's own platform and cross-compile, rather than building
# each platform under QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY src/go.mod .
COPY src/go.sum .
RUN go mod download

COPY src/ .

# Tests gate the publish: a failure here fails the image build.
RUN go vet ./... && go test ./...

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o nut-relay "./cmd"

FROM scratch

COPY --from=builder /app/nut-relay /nut-relay

# No shell, no files to write, no reason to run as root.
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/nut-relay"]
