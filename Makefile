.PHONY: tidy build run docker

tidy:
	go mod tidy

build:
	go build ./cmd/rehydrator

run:
	go run ./cmd/rehydrator

docker:
	docker build -t rehydrator:dev .
