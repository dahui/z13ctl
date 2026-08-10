# --match 'v*' restricts this to main-module tags. The api submodule is tagged
# api/vX.Y.Z in the same repository, and those tags land on more recent commits
# than the last release — without the filter, git describe picks one and the
# binary reports "api/v1.1.7" as its own version.
VERSION := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X github.com/dahui/voltaire/v2/internal/version.Version=$(VERSION)

SYSTEMD_USER_DIR  := $(HOME)/.config/systemd/user
SYSTEMD_SYSTEM_DIR := /etc/systemd/system

# The pre-2.0 user units, disabled and removed by every install-service run.
# z13gui.service is in the list because voltaire-gui.service would otherwise
# race it: both are WantedBy=graphical-session.target, and with the
# compatibility symlinks installed ExecStart=z13gui resolves through
# /usr/local/bin to voltaire-gui itself — so the same drawer is started twice
# under two unit names, and the one a "systemctl --user restart voltaire-gui"
# refreshes is not necessarily the one that is running.
LEGACY_USER_UNITS := z13ctl.socket z13ctl.service z13gui.service

HIDBLOCKER_DIR := internal/gui/gamepad/hidblocker

# HERMETIC_PKGS is every package that compiles without CGO and GTK4 headers,
# derived rather than hand-listed so a newly added package is tested
# automatically. internal/gui (the cgo island) and voltaire-gui (the main
# package importing it) are excluded by construction — that is the boundary:
# widgets there, decisions in the pure packages.
HERMETIC_PKGS := $(shell go list ./... 2>/dev/null | grep -v -e '/internal/gui' -e '/voltaire-gui')

.PHONY: build build-gui build-all test race fmt-check cover lint mod-tidy snapshot release install install-service uninstall-service install-perms-service uninstall-perms-service docs docs-api docs-build clean help

## build: compile voltaire with version from git tags
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o voltaire .

## build-gui: compile voltaire-gui (needs gtk4 + gtk4-layer-shell headers).
## The binary lands inside the source dir — a root dir and a root file cannot
## share the name voltaire-gui, and the directory keeps the plan's layout.
build-gui:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o voltaire-gui/voltaire-gui ./voltaire-gui

## build-all: compile both binaries — the pair that ships as one package
# This is what "install" wants. "make build" alone leaves whatever voltaire-gui
# binary is already in the tree, and installing that beside a freshly built
# daemon is exactly the CLI/GUI version skew that shipping one package exists to
# make impossible.
build-all: build build-gui

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

## install: install voltaire (and voltaire-gui if built) to /usr/local/bin (requires sudo, build-all first)
install:
	install -Dm755 voltaire /usr/local/bin/voltaire
# Compatibility symlinks for the pre-2.0 command names, matching what the
# distribution packages ship. Replace a stale 1.x *binary* at these paths as
# well: /usr/local/bin precedes /usr/bin, so a leftover from a pre-rename
# "make install" would shadow the packaged symlink and keep answering scripts
# with 1.x behaviour forever. That precedence also means these symlinks decide
# what a *unit* named ExecStart=z13gui runs — see LEGACY_USER_UNITS.
	ln -sfn voltaire /usr/local/bin/z13ctl
# The GUI is optional here — a headless install legitimately wants the CLI
# alone — but optional must not mean silently stale, and both halves of the old
# "[ -f x ] && install ... || true" were wrong for that: it reported nothing
# when the binary was absent, and the trailing "|| true" swallowed a genuine
# install failure with it. The version is read out of the binary rather than by
# running it, because this recipe runs as root and voltaire-gui's first
# statement is theme.MigrateFromZ13gui(); grep needs -a since these are ELF.
	@if [ ! -f voltaire-gui/voltaire-gui ]; then \
		echo "NOTE: voltaire-gui is not built — installing the CLI only."; \
		echo "      Run 'make build-all' to include the drawer."; \
	else \
		install -Dm755 voltaire-gui/voltaire-gui /usr/local/bin/voltaire-gui; \
		ln -sfn voltaire-gui /usr/local/bin/z13gui; \
		if [ "$(VERSION)" != "dev" ] && ! grep -aqF "$(VERSION)" voltaire-gui/voltaire-gui; then \
			echo "WARNING: voltaire-gui was built from a different tree than voltaire ($(VERSION))."; \
			echo "         Run 'make build-all' and install again — a skewed pair is not a"; \
			echo "         configuration this project tests."; \
		fi; \
	fi

## install-service: install and enable the voltaire systemd user units, daemon and GUI (disables the pre-2.0 z13ctl/z13gui units)
install-service:
	-systemctl --user disable --now $(LEGACY_USER_UNITS) 2>/dev/null
	rm -f $(addprefix $(SYSTEMD_USER_DIR)/,$(LEGACY_USER_UNITS))
	install -Dm644 contrib/systemd/user/voltaire.socket $(SYSTEMD_USER_DIR)/voltaire.socket
	install -Dm644 contrib/systemd/user/voltaire.service $(SYSTEMD_USER_DIR)/voltaire.service
# voltaire-gui.service is packaged in contrib/ and enabled by every distribution
# package, but no make target installed it — so a source install had no drawer
# unit at all and "systemctl --user restart voltaire-gui" failed with "unit not
# found" while the pre-2.0 z13gui.service quietly kept the drawer running.
	install -Dm644 contrib/systemd/user/voltaire-gui.service $(SYSTEMD_USER_DIR)/voltaire-gui.service
	systemctl --user daemon-reload
	systemctl --user enable --now voltaire.socket voltaire.service
	systemctl --user enable voltaire-gui.service
# "systemctl --user disable" removes symlinks under ~/.config/systemd/user and
# nothing else. The pre-2.0 packages enable their units with --global, which
# writes /etc/systemd/user/*.target.wants — root-owned, outside this target's
# reach, and reinstated at every login. The disable above therefore *looks*
# like it worked (the unit does stop) while is-enabled still reports enabled.
# Saying so here is the difference between one command and a long evening.
	@for u in $(LEGACY_USER_UNITS); do \
		if [ "$$(systemctl --user is-enabled $$u 2>/dev/null)" = "enabled" ]; then \
			echo "WARNING: $$u is still enabled system-wide and returns at next login."; \
			echo "         Uninstall the pre-2.0 package, or: sudo systemctl --global disable $$u"; \
		fi; \
	done
# Start the drawer only where it can run: voltaire-gui.service is
# PartOf=graphical-session.target, so starting it from a TTY only fails.
	@if systemctl --user --quiet is-active graphical-session.target 2>/dev/null; then \
		systemctl --user restart voltaire-gui.service && echo "voltaire-gui restarted."; \
	else \
		echo "No graphical session — voltaire-gui.service starts at next login."; \
	fi
	@echo "Units installed. Verify with 'systemctl --user status voltaire.service voltaire-gui.service'."

## uninstall-service: stop and remove the voltaire systemd user units
uninstall-service:
	-systemctl --user disable --now voltaire.socket voltaire.service voltaire-gui.service
	rm -f $(SYSTEMD_USER_DIR)/voltaire.socket $(SYSTEMD_USER_DIR)/voltaire.service \
	      $(SYSTEMD_USER_DIR)/voltaire-gui.service
	systemctl --user daemon-reload
	@echo "Units removed."

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
