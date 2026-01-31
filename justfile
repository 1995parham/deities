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
    go build -o {{ BINARY }} .

# Run the application
run: build
    ./{{ BINARY }} -config config.toml

# Clean build artifacts
clean:
    rm -f {{ BINARY }}
    go clean

# Run tests
test:
    go test -v ./...

# Build Docker image
docker-build:
    docker build -t {{ IMAGE_NAME }}:{{ IMAGE_TAG }} .

# Push Docker image (update with your registry)
docker-push: docker-build
    docker push {{ IMAGE_NAME }}:{{ IMAGE_TAG }}

# Install the binary
install:
    go install .

# Run linter
lint:
    golangci-lint run

# Update packages
update:
    go get -u

# Build for multiple platforms
build-all:
    GOOS=linux GOARCH=amd64 go build -o {{ BINARY }}-linux-amd64 .
    GOOS=darwin GOARCH=amd64 go build -o {{ BINARY }}-darwin-amd64 .
    GOOS=darwin GOARCH=arm64 go build -o {{ BINARY }}-darwin-arm64 .
    GOOS=windows GOARCH=amd64 go build -o {{ BINARY }}-windows-amd64.exe .

# Start E2E test environment
e2e-up:
    docker compose -f docker-compose.e2e.yaml up -d --build

# Run E2E tests
e2e-test: e2e-up
    ./scripts/e2e-test.sh

# Stop E2E test environment
e2e-down:
    docker compose -f docker-compose.e2e.yaml down -v

# View E2E logs
e2e-logs:
    docker compose -f docker-compose.e2e.yaml logs -f
