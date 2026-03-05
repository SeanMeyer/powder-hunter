FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${VERSION}" -o /powder-hunter ./cmd/powder-hunter/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata && mkdir -p /data
COPY --from=builder /powder-hunter /usr/local/bin/powder-hunter
ENTRYPOINT ["powder-hunter"]
