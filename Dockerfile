FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./cmd/webhook

FROM alpine:3.21
WORKDIR /
COPY --from=builder /app/webhook /webhook
ENTRYPOINT ["/webhook"]