FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./cmd/webhook

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/webhook /webhook
USER nonroot:nonroot
ENTRYPOINT ["/webhook"]