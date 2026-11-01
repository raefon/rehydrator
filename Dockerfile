FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
ARG VERSION=dev

COPY go.mod ./
RUN go mod download

COPY . .
COPY --from=frontend /internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -ldflags="-X main.version=${VERSION}" -o /out/rehydrator ./cmd/rehydrator

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser
USER appuser
COPY --from=build /out/rehydrator /usr/local/bin/rehydrator
ENTRYPOINT ["/usr/local/bin/rehydrator"]
