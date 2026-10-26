.PHONY: test check build

test:
	go test -race ./...

check:
	go vet ./...

build:
	go build -o bin/monitor-agent ./cmd/monitor-agent
	@if [ "$$(uname -s)" = "Darwin" ]; then ./scripts/build-nowplaying-macos.sh; fi

.PHONY: build-nowplaying-macos
build-nowplaying-macos:
	./scripts/build-nowplaying-macos.sh
