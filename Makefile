VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X z13ctl/cmd.Version=$(VERSION)

SYSTEMD_USER_DIR  := $(HOME)/.config/systemd/user
SYSTEMD_SYSTEM_DIR := /etc/systemd/system

.PHONY: build test cover lint snapshot release install-service uninstall-service install-perms-service uninstall-perms-service clean help

## build: compile z13ctl with version from git tags
build:
	go build -ldflags "$(LDFLAGS)" -o z13ctl .

## test: run all tests
test:
	go test ./...

## cover: run tests with coverage report
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## snapshot: build a local snapshot release via goreleaser (no publish)
snapshot:
	goreleaser release --snapshot --clean

## release: publish a release via goreleaser (requires a clean git tag)
release:
	goreleaser release --clean

## install-service: install and enable the z13ctl systemd user service
install-service: build
	install -Dm755 z13ctl $(DESTDIR)/usr/local/bin/z13ctl
	install -Dm644 contrib/systemd/user/z13ctl.socket $(SYSTEMD_USER_DIR)/z13ctl.socket
	install -Dm644 contrib/systemd/user/z13ctl.service $(SYSTEMD_USER_DIR)/z13ctl.service
	systemctl --user daemon-reload
	systemctl --user enable --now z13ctl.socket
	@echo "Service installed. Run 'systemctl --user status z13ctl.socket' to verify."

## uninstall-service: stop and remove the z13ctl systemd user service
uninstall-service:
	-systemctl --user disable --now z13ctl.socket z13ctl.service
	rm -f $(SYSTEMD_USER_DIR)/z13ctl.socket $(SYSTEMD_USER_DIR)/z13ctl.service
	systemctl --user daemon-reload
	@echo "Service removed."

## install-perms-service: install system service to chmod battery sysfs attr on boot (requires sudo)
install-perms-service:
	install -Dm644 contrib/systemd/system/z13ctl-perms.service $(SYSTEMD_SYSTEM_DIR)/z13ctl-perms.service
	systemctl daemon-reload
	systemctl enable --now z13ctl-perms.service
	@echo "Permissions service installed. Run 'systemctl status z13ctl-perms' to verify."

## uninstall-perms-service: remove the battery sysfs permissions service (requires sudo)
uninstall-perms-service:
	-systemctl disable --now z13ctl-perms.service
	rm -f $(SYSTEMD_SYSTEM_DIR)/z13ctl-perms.service
	systemctl daemon-reload
	@echo "Permissions service removed."

## clean: remove all generated build and test artifacts
clean:
	rm -f z13ctl
	rm -rf dist/
	find . -name '*.test' -delete
	find . -name 'coverage.out' -o -name 'coverage.*' -o -name '*.coverprofile' -o -name 'profile.cov' | xargs rm -f

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
