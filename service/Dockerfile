# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o conway-life .

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S rdapp && adduser -S rdapp -G rdapp
USER rdapp

WORKDIR /app

COPY --from=builder /app/conway-life .

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./conway-life"]
