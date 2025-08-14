FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./cmd/webhook

FROM alpine:3.22
WORKDIR /
COPY --from=builder /app/webhook /webhook
ENTRYPOINT ["/webhook"]