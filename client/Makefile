.PHONY: test test-fast check build build-app

test:
	go test -race ./...

# Skips simulator/pipe-backed LAN and serial integration tests.
test-fast:
	go test -short ./...

check:
	go vet ./...

build:
	go build -o bin/monitor-agent ./cmd/monitor-agent
	@if [ "$$(uname -s)" = "Darwin" ]; then ./scripts/build-nowplaying-macos.sh; fi

.PHONY: build-app-darwin
build-app-darwin:
	./scripts/build-darwin-app.sh

.PHONY: build-nowplaying-macos
build-nowplaying-macos:
	./scripts/build-nowplaying-macos.sh

.PHONY: test-collector-e2e
test-collector-e2e:
	python3 tests/e2e/collector_loopback.py --evidence "$${TMPDIR:-/tmp}/esp32-collector-e2e.json"

.PHONY: build-appimage-linux
build-appimage-linux:
	./scripts/build-linux-appimage.sh

# Release path: builds inside the pinned Ubuntu 24.04 environment so the
# artifact keeps the documented glibc/GTK/WebKit baseline.
.PHONY: build-appimage-linux-repro
build-appimage-linux-repro:
	./scripts/build-linux-appimage-repro.sh

.PHONY: check-linux-baseline
check-linux-baseline:
	./scripts/check-linux-baseline.sh build/linux/ESP32-Monitor-$$(uname -m).AppImage

.PHONY: test-appimage-linux
test-appimage-linux:
	python3 tests/test_appimage_layout.py build/linux/ESP32-Monitor-$$(uname -m).AppImage

.PHONY: ci-linux
ci-linux:
	./scripts/ci-linux.sh

.PHONY: build-headless-linux
build-headless-linux:
	./scripts/build-linux-headless.sh
