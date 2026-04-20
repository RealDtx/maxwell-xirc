.PHONY: build test clean docker run deploy

BINARY=xirc
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO ?= $(HOME)/go-install/go/bin/go

build:
	$(GO) build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) .

test:
	$(GO) test ./...

test-verbose:
	$(GO) test -v ./...

# Cross-compile for Raspberry Pi 4 (ARM64)
build-pi:
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags="-s -w" -o $(BINARY)-arm64 .

# Cross-compile for Raspberry Pi 3 (ARM)
build-pi3:
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build -ldflags="-s -w" -o $(BINARY)-arm .

docker:
	docker build -f deploy/Dockerfile -t xirc .

docker-up:
	cd deploy && docker compose up -d

docker-up-mariadb:
	cd deploy && docker compose --profile mariadb up -d

docker-down:
	cd deploy && docker compose down

run: build
	./$(BINARY) --config config.yaml

PI_HOST ?= 192.168.20.2
PI_USER ?= pi
PI_CONFIG ?=

deploy: build-pi
	rsync -avz --progress $(BINARY)-arm64 "$(PI_USER)@$(PI_HOST)":/opt/xirc/xirc
	rsync -avz deploy/xirc.service "$(PI_USER)@$(PI_HOST)":/tmp/
	ssh "$(PI_USER)@$(PI_HOST)" "sudo cp /tmp/xirc.service /etc/systemd/system/xirc.service && sudo systemctl daemon-reload"
	rsync -avz deploy/nginx-xirc.conf "$(PI_USER)@$(PI_HOST)":/tmp/
	ssh "$(PI_USER)@$(PI_HOST)" "sudo cp /tmp/nginx-xirc.conf /etc/nginx/sites-available/xirc && sudo nginx -t && sudo systemctl reload nginx"
ifdef PI_CONFIG
	rsync -avz $(PI_CONFIG) "$(PI_USER)@$(PI_HOST)":/opt/xirc/config.yaml
endif
	ssh "$(PI_USER)@$(PI_HOST)" "sudo systemctl restart xirc"

clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-arm
