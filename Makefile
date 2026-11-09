.DEFAULT_GOAL := start

.PHONY: start deps build run install update test clean

# Default: install deps, build, and run so typing `make` gets going immediately
start: build run

# Download and sync dependencies
deps:
	go mod download
	go mod tidy

# Build the agenttree binary
build: deps
	@mkdir -p bin
	go build -o bin/agenttree ./cmd/agenttree

# Run the compiled binary
run:
	./bin/agenttree

# Install binary to GOPATH/bin
install: deps
	go install ./cmd/agenttree

# Update dependencies to latest and tidy
update:
	go get -u ./...
	go mod tidy

# Run test suite
test:
	go test ./... -v

# Clean build artifacts
clean:
	rm -rf bin
