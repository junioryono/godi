.PHONY: all test test-verbose test-race test-cover bench lint fmt clean help docs

# Default target
all: test

# Run tests
test:
	@echo "Running tests..."
	@go test ./...

# Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	@go test -v ./...

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	@go test -race ./...

# Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Run linting
lint:
	@echo "Running linters..."
	@golangci-lint run || (echo "golangci-lint not installed, running go vet..." && go vet ./...)

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f coverage.out coverage.html
	@rm -f *.test
	@rm -f *.prof
	@go clean

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

# Verify module
verify:
	@echo "Verifying module..."
	@go mod verify

# Run CI checks locally
ci: fmt lint test-race test-cover
	@echo "All CI checks passed!"

docs:
	@echo "Building documentation..."
	@$(MAKE) -C docs build

# Show help
help:
	@echo "Available targets:"
	@echo "  make test          - Run tests"
	@echo "  make test-verbose  - Run tests with verbose output"
	@echo "  make test-race     - Run tests with race detector"
	@echo "  make test-cover    - Run tests with coverage report"
	@echo "  make bench         - Run benchmarks"
	@echo "  make lint          - Run linters"
	@echo "  make fmt           - Format code"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make deps          - Install dependencies"
	@echo "  make update-deps   - Update dependencies"
	@echo "  make verify        - Verify module"
	@echo "  make ci            - Run all CI checks"
	@echo "  make docs          - Build documentation"
	@echo "  make help          - Show this help message"