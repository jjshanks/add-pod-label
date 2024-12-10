FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./pkg/webhook/cmd

FROM alpine:3.21
WORKDIR /
COPY --from=builder /app/webhook /webhook
ENTRYPOINT ["/webhook"]