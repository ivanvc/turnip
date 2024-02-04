FROM golang:alpine

WORKDIR /app
COPY go.mod go.sum .
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
