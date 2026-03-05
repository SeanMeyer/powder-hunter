.PHONY: build test lint clean

VERSION ?= dev

build:
	CGO_ENABLED=0 go build -ldflags="-X main.version=$(VERSION)" -o powder-hunter ./cmd/powder-hunter/

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f powder-hunter
