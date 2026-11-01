.PHONY: tidy build web-build release-build run docker version

tidy:
	go mod tidy

web-build:
	cd web && npm ci && npm run build

release-build: web-build build

build:
	go build -ldflags="-X main.version=$$(cat VERSION)" -o rehydrator ./cmd/rehydrator

version:
	go run ./cmd/rehydrator --version

run:
	go run ./cmd/rehydrator --config ./rehydrator.yaml

docker:
	docker build -t rehydrator:dev .
