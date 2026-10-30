.PHONY: build build-linux-amd64 test-ut test-race test-api test-v2 test-tui vet

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/monitor-server ./cmd/monitor-server

build-linux-amd64:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/monitor-server-linux-amd64 ./cmd/monitor-server

test-ut:
	go test -count=1 -coverprofile=coverage.out ./...

test-race:
	go test -race -count=1 ./...

# Real loopback HTTP + temporary SQLite; opt in separately from UT.
test-api:
	go test -tags=integration -count=1 -v ./tests/integration

# Real Client Agent + production Server, temporary state and loopback only.
test-v2: build
	python3 tests/e2e/v2_loopback.py --server bin/monitor-server

# Local terminal only; install tests/e2e/requirements-tui.txt in a venv first.
test-tui: build
	python3 tests/e2e/tui_terminal.py --server bin/monitor-server

vet:
	go vet ./...
