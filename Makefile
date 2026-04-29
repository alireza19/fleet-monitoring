.PHONY: run test test-race lint tidy build

run:
	go run ./cmd/server

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

build:
	go build -o bin/fleet-server ./cmd/server
