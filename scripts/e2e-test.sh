#!/bin/bash
#
# E2E Test Script for Deities
#
# This script:
# 1. Builds and pushes a test image to the local registry
# 2. Waits for deities to detect the initial image
# 3. Pushes an updated image
# 4. Verifies deities triggers a deployment restart
#
# Prerequisites:
#   docker compose -f docker-compose.e2e.yaml up -d

set -euo pipefail

REGISTRY="localhost:5000"
IMAGE_NAME="test-app"
KUBECONFIG_PATH=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Wait for registry to be ready
wait_for_registry() {
    log_info "Waiting for registry to be ready..."
    for i in {1..30}; do
        if curl -s "http://${REGISTRY}/v2/" > /dev/null 2>&1; then
            log_info "Registry is ready"
            return 0
        fi
        sleep 1
    done
    log_error "Registry did not become ready"
    return 1
}

# Extract kubeconfig from k3s container
setup_kubeconfig() {
    log_info "Setting up kubeconfig..."
    KUBECONFIG_PATH=$(mktemp)
    docker cp deities-k3s:/output/kubeconfig.yaml "$KUBECONFIG_PATH"
    # Fix server address for host access
    sed -i 's/k3s/localhost/g' "$KUBECONFIG_PATH"
    export KUBECONFIG="$KUBECONFIG_PATH"
    log_info "Kubeconfig ready at $KUBECONFIG_PATH"
}

# Build and push a test image with a unique label
push_test_image() {
    local version=$1
    log_info "Building and pushing test image version: $version"

    # Create a simple Dockerfile
    local tmpdir=$(mktemp -d)
    cat > "$tmpdir/Dockerfile" <<EOF
FROM alpine:3.21
RUN echo "Version: $version - Built at: $(date -u +%Y-%m-%dT%H:%M:%SZ)" > /version.txt
CMD ["cat", "/version.txt"]
EOF

    docker build -t "${REGISTRY}/${IMAGE_NAME}:latest" "$tmpdir"
    docker push "${REGISTRY}/${IMAGE_NAME}:latest"

    rm -rf "$tmpdir"
    log_info "Pushed ${REGISTRY}/${IMAGE_NAME}:latest (version: $version)"
}

# Get current image digest from registry
get_registry_digest() {
    # Use HEAD request to get manifest digest (architecture-agnostic)
    curl -s -I \
        -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
        "http://${REGISTRY}/v2/${IMAGE_NAME}/manifests/latest" 2>/dev/null | \
        grep -i "docker-content-digest" | awk '{print $2}' | tr -d '\r'
}

# Get deployment restart annotation
get_deployment_restart_time() {
    kubectl get deployment test-app -o jsonpath='{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}' 2>/dev/null || echo ""
}

# Wait for deployment to be restarted
wait_for_restart() {
    local initial_time=$1
    local timeout=${2:-60}

    log_info "Waiting for deployment restart (timeout: ${timeout}s)..."
    for i in $(seq 1 $timeout); do
        local current_time=$(get_deployment_restart_time)
        if [[ -n "$current_time" && "$current_time" != "$initial_time" ]]; then
            log_info "Deployment restarted at: $current_time"
            return 0
        fi
        sleep 1
    done

    log_error "Deployment was not restarted within ${timeout}s"
    return 1
}

# Main test flow
main() {
    log_info "Starting E2E test for Deities"

    # Setup
    wait_for_registry
    setup_kubeconfig

    # Verify k3s is accessible
    log_info "Verifying Kubernetes cluster..."
    kubectl get nodes

    # Push initial image
    push_test_image "v1"
    local digest_v1=$(get_registry_digest)
    log_info "Initial digest: $digest_v1"

    # Wait for deities to pick up the initial state
    log_info "Waiting for deities to sync initial state (30s)..."
    sleep 30

    # Record current restart time
    local initial_restart=$(get_deployment_restart_time)
    log_info "Initial restart annotation: ${initial_restart:-<none>}"

    # Push updated image
    push_test_image "v2"
    local digest_v2=$(get_registry_digest)
    log_info "Updated digest: $digest_v2"

    if [[ "$digest_v1" == "$digest_v2" ]]; then
        log_error "Digests are the same - image was not updated properly"
        exit 1
    fi

    # Wait for deities to detect the change and restart deployment
    if wait_for_restart "$initial_restart" 120; then
        log_info "E2E test PASSED: Deities detected image update and restarted deployment"
    else
        log_error "E2E test FAILED: Deployment was not restarted"
        log_info "Checking deities logs..."
        docker logs deities-app --tail 50
        exit 1
    fi

    # Cleanup
    if [[ -n "$KUBECONFIG_PATH" && -f "$KUBECONFIG_PATH" ]]; then
        rm -f "$KUBECONFIG_PATH"
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up E2E environment..."
    docker compose -f docker-compose.e2e.yaml down -v 2>/dev/null || true
}

# Parse arguments
case "${1:-}" in
    cleanup)
        cleanup
        ;;
    *)
        main
        ;;
esac
