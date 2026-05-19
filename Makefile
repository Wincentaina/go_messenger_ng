.PHONY: build run-server run-client test docker-up docker-down certs clean

## Build both binaries into ./bin/
build:
	go build -o bin/server ./cmd/server
	go build -o bin/client ./cmd/client

## Run the server (requires certs + postgres running)
run-server: build
	./bin/server -config config/server.yaml

## Run the client TUI
run-client: build
	./bin/client -config config/client.yaml

## Run all tests
test:
	go test ./...

## Start infrastructure (postgres) in background
docker-up:
	docker compose up -d postgres

## Start everything including the server container
docker-up-all:
	docker compose up -d

## Stop and remove containers (data volume preserved)
docker-down:
	docker compose down

## Generate self-signed TLS certs for local development
certs:
	bash scripts/gen_certs.sh

## Remove compiled binaries
clean:
	rm -rf bin/*
