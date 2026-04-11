FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o xirc .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -h /app xirc
USER xirc
WORKDIR /app

COPY --from=builder /build/xirc .
COPY --from=builder /build/config.yaml ./data/config.yaml

EXPOSE 8085
VOLUME ["/app/data", "/srv/dlna/media", "/srv/downloads"]

ENTRYPOINT ["./xirc", "--config", "/app/data/config.yaml"]
