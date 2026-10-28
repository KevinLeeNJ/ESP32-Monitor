.PHONY: test check build build-app

test:
	go test -race ./...

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
