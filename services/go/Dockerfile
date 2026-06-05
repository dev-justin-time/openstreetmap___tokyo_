# Build stage
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /app/gateway .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget sqlite-libs
COPY --from=builder /app/gateway /usr/local/bin/gateway
EXPOSE 8080 8081 8082
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1
USER nobody
ENTRYPOINT ["gateway"]
