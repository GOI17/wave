#!/bin/bash

# Wave Build and Test Script
# Complete build, test, and release automation

set -e

VERSION="1.1.3"
PROJECT="Wave"
BINARY="wave"
BUILD_DIR="./build"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Main workflow
main() {
    print_header "$PROJECT v$VERSION Build & Test Script"

    case "${1:-help}" in
        build)
            cmd_build
            ;;
        test)
            cmd_test
            ;;
        coverage)
            cmd_coverage
            ;;
        clean)
            cmd_clean
            ;;
        release)
            cmd_release
            ;;
        docker)
            cmd_docker
            ;;
        all)
            cmd_build && cmd_test && cmd_coverage
            ;;
        help)
            print_help
            ;;
        *)
            print_error "Unknown command: $1"
            print_help
            exit 1
            ;;
    esac
}

cmd_build() {
    print_info "Building $BINARY v$VERSION..."
    
    mkdir -p "$BUILD_DIR"
    
    # Build for macOS
    GOOS=darwin GOARCH=arm64 go build \
		-ldflags="-s -w -X wave/ui/cli.Version=$VERSION" \
        -o "$BUILD_DIR/$BINARY-darwin-arm64" \
        ./cmd/wave
    
    GOOS=darwin GOARCH=amd64 go build \
		-ldflags="-s -w -X wave/ui/cli.Version=$VERSION" \
        -o "$BUILD_DIR/$BINARY-darwin-amd64" \
        ./cmd/wave
    
    # Also build for current platform
    go build -ldflags="-s -w -X wave/ui/cli.Version=$VERSION" -o "$BINARY" ./cmd/wave
    
    print_success "Build complete"
    print_info "Output:"
    ls -lh "$BINARY"
    ls -lh "$BUILD_DIR/"
}

cmd_test() {
    print_info "Running tests..."
    
    # Run all tests with verbose output
    go test -v -race ./...
    
    print_success "Tests passed"
}

cmd_coverage() {
    print_info "Generating coverage report..."
    
    go test -v -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    
    # Show coverage summary
    go tool cover -func=coverage.out | tail -1
    
    print_success "Coverage report generated: coverage.html"
}

cmd_clean() {
    print_info "Cleaning build artifacts..."
    
    rm -rf "$BUILD_DIR"
    rm -f "$BINARY"
    rm -f coverage.out coverage.html
    go clean
    
    print_success "Clean complete"
}

cmd_release() {
    print_header "Creating Release v$VERSION"
    
    # Verify tests pass
    cmd_test || exit 1
    
    # Build release binaries
    cmd_build
    
    # Create checksums
    print_info "Creating checksums..."
    cd "$BUILD_DIR"
    shasum -a 256 * > SHA256SUMS
    cd ..
    
    # Create release archive
    RELEASE_FILE="wave-v${VERSION}.tar.gz"
    print_info "Creating release archive: $RELEASE_FILE"
    tar czf "$RELEASE_FILE" "$BUILD_DIR"/ README.md RELEASE_NOTES.md
    
    print_success "Release created: $RELEASE_FILE"
    print_info "Contents:"
    tar tzf "$RELEASE_FILE" | head -20
}

cmd_docker() {
    print_info "Building Docker image..."
    
    docker build -t wave:latest .
    docker tag wave:latest wave:v$VERSION
    
    print_success "Docker image built"
    print_info "Test with: docker run -it wave:latest --help"
    
    # Optional: run tests in container
    if [[ "${2:-}" == "test" ]]; then
        print_info "Running tests in container..."
        docker run -it wave:latest test
    fi
}

cmd_install() {
    print_info "Installing Wave..."
    
    go build -o /usr/local/bin/wave ./cmd/wave
    
    print_success "Installed to /usr/local/bin/wave"
    print_info "Test: wave --version"
}

print_help() {
    cat << EOF
$PROJECT v$VERSION - Build & Test Script

USAGE:
    ./build.sh [COMMAND]

COMMANDS:
    build       Build binaries for macOS (ARM64 + x86_64)
    test        Run all tests with race detection
    coverage    Generate test coverage report
    clean       Remove build artifacts
    release     Create release (build + test + package)
    docker      Build Docker image
    install     Install to /usr/local/bin
    all         Build + test + coverage
    help        Show this help message

EXAMPLES:
    ./build.sh build           # Build for current platform
    ./build.sh test            # Run tests
    ./build.sh release         # Create full release
    ./build.sh all             # Full build + test + coverage
    ./build.sh docker test     # Build Docker and run tests

ENVIRONMENT:
    VERSION     Override version (default: $VERSION)
    BUILD_DIR   Override build directory (default: $BUILD_DIR)

EOF
}

# Run main function
main "$@"
