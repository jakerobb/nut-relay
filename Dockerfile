FROM golang:1.26.2 AS builder

WORKDIR /app

COPY src/go.mod .
COPY src/go.sum .
RUN go mod download

COPY src/ .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o nut-influx-relay "./cmd"

FROM scratch

COPY --from=builder /app/nut-influx-relay /nut-influx-relay

EXPOSE 8080

ENTRYPOINT ["/nut-influx-relay"]
