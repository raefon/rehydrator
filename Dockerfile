FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/rehydrator ./cmd/rehydrator

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
USER appuser
COPY --from=build /out/rehydrator /usr/local/bin/rehydrator
ENTRYPOINT ["/usr/local/bin/rehydrator"]
