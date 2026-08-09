# --match 'v*' restricts this to main-module tags. The api submodule is tagged
# api/vX.Y.Z in the same repository, and those tags land on more recent commits
# than the last release — without the filter, git describe picks one and the
# binary reports "api/v1.1.7" as its own version.
VERSION := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X github.com/dahui/voltaire/v2/internal/version.Version=$(VERSION)

SYSTEMD_USER_DIR  := $(HOME)/.config/systemd/user
SYSTEMD_SYSTEM_DIR := /etc/systemd/system

HIDBLOCKER_DIR := internal/gui/gamepad/hidblocker

# HERMETIC_PKGS is every package that compiles without CGO and GTK4 headers,
# derived rather than hand-listed so a newly added package is tested
# automatically. internal/gui (the cgo island) and voltaire-gui (the main
# package importing it) are excluded by construction — that is the boundary:
# widgets there, decisions in the pure packages.
HERMETIC_PKGS := $(shell go list ./... 2>/dev/null | grep -v -e '/internal/gui' -e '/voltaire-gui')

.PHONY: build build-gui test race fmt-check cover lint mod-tidy snapshot release install install-service uninstall-service install-perms-service uninstall-perms-service docs docs-api docs-build clean help

## build: compile voltaire with version from git tags
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o voltaire .

## build-gui: compile voltaire-gui (needs gtk4 + gtk4-layer-shell headers).
## The binary lands inside the source dir — a root dir and a root file cannot
## share the name voltaire-gui, and the directory keeps the plan's layout.
build-gui:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o voltaire-gui/voltaire-gui ./voltaire-gui

## test: run all tests (both modules — api/ is separate, so ./... misses it).
## internal/gui is the cgo island and voltaire-gui is the main package that
## imports it: neither has tests, but ./... would still compile both, dragging
## GTK4 headers into what must stay a hermetic run.
test:
	go test $(HERMETIC_PKGS)
	cd api && go test ./...

## race: run unit tests under the race detector (both modules)
race:
	go test -race $(HERMETIC_PKGS)
	cd api && go test -race ./...

## cover: run tests with coverage report
cover:
	go test -coverprofile=coverage.out $(HERMETIC_PKGS)
	go tool cover -func=coverage.out

## fmt-check: fail if any file needs gofmt (generated bpf2go bindings excluded)
fmt-check:
	@unformatted="$$(gofmt -l . | grep -v -e '^$(HIDBLOCKER_DIR)/blocker_' -e '^dist/' -e '^site/' || true)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		echo "Run: gofmt -w <file>"; \
		exit 1; \
	fi

## lint: check formatting, then run golangci-lint (both modules)
lint: fmt-check
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

API_GO_PAGE := website/src/content/docs/reference/api-go.md

## docs-api: regenerate the Go API reference page from api/ doc comments (commit the result)
docs-api:
	printf -- '---\ntitle: Go API Reference\ndescription: Generated reference for every exported type and function in the voltaire api module.\n---\n\n' > $(API_GO_PAGE)
	go run github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest ./api/... | sed '/^# api$$/d' >> $(API_GO_PAGE)

## docs: serve the website locally (run docs-api first if api/ changed)
docs:
	cd website && pnpm install && pnpm dev

## docs-build: build the website as CI does
docs-build:
	cd website && pnpm install && pnpm build

## clean: remove all generated build and test artifacts
clean:
	rm -f voltaire z13ctl voltaire-gui/voltaire-gui
	rm -rf dist/
	find . -name '*.test' -delete
	find . -name 'coverage.out' -o -name 'coverage.*' -o -name '*.coverprofile' -o -name 'profile.cov' | xargs rm -f

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
