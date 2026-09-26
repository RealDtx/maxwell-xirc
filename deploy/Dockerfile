FROM golang:1.27-alpine AS builder
ARG VERSION=dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /xirc .

FROM alpine:3.24
ARG PUID=1000
ARG PGID=1000
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g ${PGID} xirc \
 && adduser -D -H -u ${PUID} -G xirc xirc \
 && mkdir -p /data /downloads /media \
 && chown xirc:xirc /data /downloads /media
COPY --from=builder /xirc /usr/local/bin/xirc

# No config file is baked in: defaults + these env vars; mount
# /data/config.yaml to override anything.
ENV XIRC_SERVER_HOST=0.0.0.0 \
    XIRC_DATABASE_PATH=/data/xirc.db \
    XIRC_STORAGE_DOWNLOADS_DIR=/downloads \
    XIRC_STORAGE_TEMP_DIR=/downloads/.tmp \
    XIRC_STORAGE_MEDIA_DIR=/media \
    XIRC_STORAGE_CATEGORIES_FILE=/data/categories.yaml

USER xirc
WORKDIR /data
VOLUME ["/data", "/downloads", "/media"]
EXPOSE 8085
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8085/api/health || exit 1
ENTRYPOINT ["xirc", "--config", "/data/config.yaml"]
