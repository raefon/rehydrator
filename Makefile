.PHONY: tidy build run docker version

tidy:
	go mod tidy

build:
	go build -ldflags="-X main.version=$$(cat VERSION)" -o rehydrator ./cmd/rehydrator

version:
	go run ./cmd/rehydrator --version

run:
	go run ./cmd/rehydrator --config ./rehydrator.yaml

docker:
	docker build -t rehydrator:dev .
