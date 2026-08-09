# --match 'v*' restricts this to main-module tags. The api submodule is tagged
# api/vX.Y.Z in the same repository, and those tags land on more recent commits
# than the last release — without the filter, git describe picks one and the
# binary reports "api/v1.1.7" as its own version.
VERSION := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X github.com/dahui/voltaire/v2/internal/version.Version=$(VERSION)

SYSTEMD_USER_DIR  := $(HOME)/.config/systemd/user
SYSTEMD_SYSTEM_DIR := /etc/systemd/system

.PHONY: build test cover lint mod-tidy snapshot release install install-service uninstall-service install-perms-service uninstall-perms-service docs clean help

## build: compile voltaire with version from git tags
build:
	go build -ldflags "$(LDFLAGS)" -o voltaire .

## test: run all tests (both modules — api/ is separate, so ./... misses it)
test:
	go test ./...
	cd api && go test ./...

## cover: run tests with coverage report
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

## lint: run golangci-lint (both modules)
lint:
	golangci-lint run ./...
	cd api && golangci-lint run ./...

## mod-tidy: tidy go.mod for all modules in the repo
mod-tidy:
	go mod tidy
	cd api && go mod tidy

## snapshot: build a local snapshot release via goreleaser (no publish)
snapshot:
	goreleaser release --snapshot --clean

## release: publish a release via goreleaser (requires a clean git tag)
release:
	goreleaser release --clean

## install: install voltaire binary to /usr/local/bin (requires sudo, build first)
install:
	install -Dm755 voltaire /usr/local/bin/voltaire

## install-service: install and enable the voltaire systemd user service (disables pre-rename z13ctl units)
install-service:
	-systemctl --user disable --now z13ctl.socket z13ctl.service 2>/dev/null
	rm -f $(SYSTEMD_USER_DIR)/z13ctl.socket $(SYSTEMD_USER_DIR)/z13ctl.service
	install -Dm644 contrib/systemd/user/voltaire.socket $(SYSTEMD_USER_DIR)/voltaire.socket
	install -Dm644 contrib/systemd/user/voltaire.service $(SYSTEMD_USER_DIR)/voltaire.service
	systemctl --user daemon-reload
	systemctl --user enable --now voltaire.socket voltaire.service
	@echo "Service installed. Run 'systemctl --user status voltaire.service' to verify."

## uninstall-service: stop and remove the voltaire systemd user service
uninstall-service:
	-systemctl --user disable --now voltaire.socket voltaire.service
	rm -f $(SYSTEMD_USER_DIR)/voltaire.socket $(SYSTEMD_USER_DIR)/voltaire.service
	systemctl --user daemon-reload
	@echo "Service removed."

## install-perms-service: install system service to chmod battery + firmware-attributes sysfs on boot (requires sudo; disables pre-rename z13ctl unit)
install-perms-service:
	-systemctl disable --now z13ctl-perms.service 2>/dev/null
	rm -f $(SYSTEMD_SYSTEM_DIR)/z13ctl-perms.service
	install -Dm644 contrib/systemd/system/voltaire-perms.service $(SYSTEMD_SYSTEM_DIR)/voltaire-perms.service
	systemctl daemon-reload
	systemctl enable --now voltaire-perms.service
	@echo "Permissions service installed. Run 'systemctl status voltaire-perms' to verify."

## uninstall-perms-service: remove the sysfs permissions service (requires sudo)
uninstall-perms-service:
	-systemctl disable --now voltaire-perms.service
	rm -f $(SYSTEMD_SYSTEM_DIR)/voltaire-perms.service
	systemctl daemon-reload
	@echo "Permissions service removed."

## docs: generate API reference and serve mkdocs locally
docs:
	go run github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest ./api/... > docs/api-reference.md
	mkdocs serve

## clean: remove all generated build and test artifacts
clean:
	rm -f voltaire z13ctl
	rm -rf dist/
	find . -name '*.test' -delete
	find . -name 'coverage.out' -o -name 'coverage.*' -o -name '*.coverprofile' -o -name 'profile.cov' | xargs rm -f

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
