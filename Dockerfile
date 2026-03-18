FROM golang:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o http-host-proxy .

FROM registry.suse.com/bci/bci-minimal:15.7

WORKDIR /app

COPY --from=builder /app/http-host-proxy /app/http-host-proxy

EXPOSE 8080

ENTRYPOINT ["/app/http-host-proxy"]
