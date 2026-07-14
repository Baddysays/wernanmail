# Build stage
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api

# Runtime — small, RAM-conscious
FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
  && adduser -D -H -u 10001 app
USER app
WORKDIR /app
COPY --from=build /out/api /app/api
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
CMD ["/app/api"]
