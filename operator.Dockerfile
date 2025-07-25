FROM golang:1.24.3 AS builder

WORKDIR /workspace

ARG PLATFORM=arm64

ENV GOCACHE=/build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/operator/ ./cmd/operator/
COPY pkg/ ./pkg/
COPY internal ./internal
COPY api ./api


RUN --mount=type=cache,target=/build GOOS=linux GOARCH=${PLATFORM} CGO_ENABLED=0 go build -o manager ./cmd/operator

FROM ubuntu:latest

USER 1000:1000

WORKDIR /

COPY --from=builder /workspace/manager .

ENTRYPOINT ["/manager"]



