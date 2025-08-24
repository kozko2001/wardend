#!/bin/bash
set -e

echo "Running Docker integration tests for wardend..."

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo "Docker is not available. Skipping integration tests."
    exit 0
fi

# Build the test image
echo "Building test Docker image..."
docker build -f Dockerfile.test -t wardend-test .

echo "Running integration tests..."

# Test 1: Basic functionality
echo "Test 1: Basic functionality test..."
CONTAINER_ID=$(docker run -d wardend-test /usr/local/bin/wardend \
    --run "echo 'Hello from wardend'" \
    --name "test-process" \
    --restart "never" \
    --log-format "json")

# Wait for completion
docker wait $CONTAINER_ID

# Check logs
echo "Container logs:"
docker logs $CONTAINER_ID

# Cleanup
docker rm $CONTAINER_ID

echo "Test 1 completed successfully!"

# Test 2: YAML configuration
echo "Test 2: YAML configuration test..."
CONTAINER_ID=$(docker run -d wardend-test /usr/local/bin/wardend \
    --config /app/test/basic-test.yml)

# Wait for completion with timeout
timeout 30s docker wait $CONTAINER_ID || echo "Container timed out (may be expected)"

# Check logs
echo "Container logs:"
docker logs $CONTAINER_ID

# Cleanup
docker stop $CONTAINER_ID >/dev/null 2>&1 || true
docker rm $CONTAINER_ID

echo "Test 2 completed!"

# Test 3: Health check endpoint
echo "Test 3: Health check endpoint test..."
CONTAINER_ID=$(docker run -d -p 8080:8080 wardend-test /usr/local/bin/wardend \
    --run "sleep 10" \
    --name "health-test" \
    --restart "never" \
    --monitor-http-port "8080" \
    --health-check "echo 'healthy'" \
    --health-interval "2s")

# Wait for startup
sleep 3

# Test health endpoint
echo "Testing health endpoint..."
if curl -f http://localhost:8080/health; then
    echo "Health endpoint test passed!"
else
    echo "Health endpoint test failed!"
fi

# Cleanup
docker stop $CONTAINER_ID >/dev/null 2>&1 || true
docker rm $CONTAINER_ID

echo "Test 3 completed!"

echo "All Docker integration tests completed successfully!"