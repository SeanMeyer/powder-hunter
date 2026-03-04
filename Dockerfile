FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /powder-hunter ./cmd/powder-hunter/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /powder-hunter /usr/local/bin/powder-hunter
ENTRYPOINT ["powder-hunter"]
