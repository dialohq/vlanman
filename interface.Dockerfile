FROM golang:1.24.3 AS builder

WORKDIR /workspace

ARG PLATFORM=arm64

ENV GOCACHE=/build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/interface/ ./cmd/interface/
COPY pkg/ ./pkg/
COPY internal ./internal
COPY api ./api


RUN --mount=type=cache,target=/build GOOS=linux GOARCH=${PLATFORM} CGO_ENABLED=0 go build -o interface ./cmd/interface

FROM ubuntu:latest

WORKDIR /

COPY --from=builder /workspace/interface .

ENTRYPOINT ["/interface"]


