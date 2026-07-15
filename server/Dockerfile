# Build every Go service once into one small shared image.
FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /out \
  && for binary in mta imapd worker admin api docker-init; do \
       CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/${binary}" "./cmd/${binary}"; \
     done

# Runtime is shared by mta, imapd, worker, admin, api, and init.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget netcat-openbsd \
  && adduser -D -H -u 10001 app \
  && mkdir -p /data && chown app:app /data
USER app
WORKDIR /app
COPY --from=build /out/ /app/
VOLUME ["/data"]
ENV DATA_DIR=/data
CMD ["/app/api"]