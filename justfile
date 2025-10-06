# Binary name
BINARY := "deities"

# Docker image
IMAGE_NAME := "deities"
IMAGE_TAG := "latest"

# Default recipe to display help
default:
    @just --list

# Build the application
build:
    go build -o {{BINARY}} .

# Run the application
run: build
    ./{{BINARY}} -config config.yaml

# Clean build artifacts
clean:
    rm -f {{BINARY}}
    go clean

# Run tests
test:
    go test -v ./...

# Install dependencies
deps:
    go mod download
    go mod tidy

# Build Docker image
docker-build:
    docker build -t {{IMAGE_NAME}}:{{IMAGE_TAG}} .

# Push Docker image (update with your registry)
docker-push: docker-build
    docker push {{IMAGE_NAME}}:{{IMAGE_TAG}}

# Install the binary
install:
    go install .

# Format code
fmt:
    go fmt ./...

# Run linter
lint:
    golangci-lint run

# Run go mod tidy
tidy:
    go mod tidy

# Build for multiple platforms
build-all:
    GOOS=linux GOARCH=amd64 go build -o {{BINARY}}-linux-amd64 .
    GOOS=darwin GOARCH=amd64 go build -o {{BINARY}}-darwin-amd64 .
    GOOS=darwin GOARCH=arm64 go build -o {{BINARY}}-darwin-arm64 .
    GOOS=windows GOARCH=amd64 go build -o {{BINARY}}-windows-amd64.exe .
