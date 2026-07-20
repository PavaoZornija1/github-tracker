# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
USER nobody

FROM runtime AS api
EXPOSE 8080
ENTRYPOINT ["/app/api"]

FROM runtime AS worker
ENTRYPOINT ["/app/worker"]
