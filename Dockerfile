# syntax=docker/dockerfile:1.7
ARG GO_IMAGE=golang:1.27.0-alpine3.24@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc
ARG RUNTIME_IMAGE=alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

FROM ${GO_IMAGE} AS build
ARG PLUGIN_VERSION=0.1.1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${PLUGIN_VERSION}" -o /out/plugin .

FROM ${RUNTIME_IMAGE}
RUN apk upgrade --no-cache && \
    addgroup -S -g 10002 plugin && adduser -S -D -H -u 10002 -G plugin plugin
COPY --from=build /out/plugin /plugin
COPY ui /ui
USER 10002:10002
EXPOSE 8080
ENTRYPOINT ["/plugin"]
