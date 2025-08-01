# Build stage
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o tgptbot ./cmd/tgptbot

# Runtime stage
FROM alpine:3.18
RUN apk add --no-cache ca-certificates
WORKDIR /data
COPY --from=builder /app/tgptbot /usr/local/bin/tgptbot
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/tgptbot"]
