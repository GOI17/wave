.PHONY: help build clean test run capture apply tidy fmt lint install release docker version

VERSION=1.0.3
BINARY=wave
BUILD_DIR=build

help:
	@echo "🌊 Wave v$(VERSION) – macOS Device Migrator"
	@echo ""
	@echo "Available targets:"
	@echo ""
	@echo "Building:"
	@echo "  make build        - Build Wave binary"
	@echo "  make install      - Install to /usr/local/bin"
	@echo "  make release      - Create release artifacts"
	@echo "  make docker       - Build Docker image"
	@echo ""
	@echo "Running:"
	@echo "  make run          - Run Wave CLI (use ARGS=... for flags)"
	@echo "  make capture      - Capture device state"
	@echo "  make apply-dry    - Test apply (dry-run)"
	@echo ""
	@echo "Testing:"
	@echo "  make test         - Run all tests"
	@echo "  make test-race    - Run tests with race detection"
	@echo "  make test-verbose - Run tests with verbose output"
	@echo "  make coverage     - Generate coverage report"
	@echo "  make benchmark    - Run benchmarks"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Run linter"
	@echo "  make tidy         - Tidy dependencies"
	@echo "  make vet          - Run go vet"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make version      - Show version"
	@echo "  make help         - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make test"
	@echo "  make run ARGS='--version'"
	@echo "  make run ARGS='capture --output state.yaml'"
	@echo "  make release"

build:
	@echo "🔨 Building Wave CLI v$(VERSION)..."
	mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w -X wave/ui/cli.Version=$(VERSION)" -o $(BINARY) ./cmd/wave
	@echo "✓ Build complete: ./$(BINARY)"

install: build
	@echo "📦 Installing to /usr/local/bin..."
	cp $(BINARY) /usr/local/bin/
	@echo "✓ Installed: /usr/local/bin/$(BINARY)"
	wave --version

run: build
	./$(BINARY) $(ARGS)

capture: build
	@echo "📸 Capturing device state..."
	./$(BINARY) capture --output state.yaml
	@echo "✓ State saved to state.yaml"
	@echo ""
	@head -20 state.yaml

apply-dry: build
	@echo "🔍 Testing migration (dry-run)..."
	./$(BINARY) apply --input state.yaml --dry-run

clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY)
	rm -f coverage.out coverage.html
	go clean
	@echo "✓ Clean complete"

test:
	@echo "🧪 Running tests..."
	go test -v ./...

test-race:
	@echo "🏎️ Running tests with race detection..."
	go test -v -race ./...

test-verbose:
	@echo "📝 Running tests (verbose)..."
	go test -v -count=1 ./...

coverage:
	@echo "📊 Generating coverage report..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"
	go tool cover -func=coverage.out | tail -1

benchmark:
	@echo "⚡ Running benchmarks..."
	go test -bench=. -benchmem ./...

tidy:
	@echo "📚 Tidying dependencies..."
	go mod tidy
	go mod verify
	@echo "✓ Dependencies tidied"

fmt:
	@echo "✨ Formatting code..."
	go fmt ./...
	@echo "✓ Code formatted"

lint:
	@echo "🔍 Running linter..."
	@command -v golangci-lint >/dev/null 2>&1 || (echo "Install golangci-lint: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...
	@echo "✓ Lint passed"

vet:
	@echo "🔎 Running go vet..."
	go vet ./...
	@echo "✓ Vet passed"

version:
	@echo "🌊 Wave v$(VERSION)"
	@go version
	@go env | grep -E "GOARCH|GOOS|GOVERSION"

release: test build
	@echo "🚀 Creating release v$(VERSION)..."
	mkdir -p $(BUILD_DIR)
	tar czf wave-v$(VERSION).tar.gz $(BINARY) README.md RELEASE_NOTES.md
	@echo "✓ Release created: wave-v$(VERSION).tar.gz"

docker:
	@echo "🐳 Building Docker image..."
	docker build -t wave:latest -t wave:v$(VERSION) .
	@echo "✓ Docker image built"
	@echo "   Test with: docker run -it wave:latest --help"

.DEFAULT_GOAL := help
