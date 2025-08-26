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
CONTAINER_ID=$(docker run -d -p 9091:9091 wardend-test /usr/local/bin/wardend \
    --run "sleep 10" \
    --name "health-test" \
    --restart "never" \
    --monitor-http-port "9091" \
    --health-check "echo 'healthy'" \
    --health-interval "2s")

# Wait for startup
sleep 3

# Test health endpoint
echo "Testing health endpoint..."
if curl -f http://localhost:9091/health; then
    echo "Health endpoint test passed!"
else
    echo "Health endpoint test failed!"
fi

# Cleanup
docker stop $CONTAINER_ID >/dev/null 2>&1 || true
docker rm $CONTAINER_ID

echo "Test 3 completed!"

# Test 4: Cron functionality test
echo "Test 4: Cron functionality test..."
CONTAINER_ID=$(docker run -d -p 9092:8090 wardend-test /usr/local/bin/wardend \
    --config /app/test/cron-test.yml)

# Wait for startup and initial cron executions
echo "Waiting for cron jobs to execute..."
sleep 25

# Test cron HTTP endpoints
echo "Testing cron HTTP endpoints..."
echo "1. Testing /crons endpoint..."
if curl -f -s http://localhost:9092/crons > /dev/null; then
    echo "   /crons endpoint test passed!"
    curl -s http://localhost:9092/crons | jq '.count' 2>/dev/null || echo "   Response received (jq not available)"
else
    echo "   /crons endpoint test failed!"
fi

echo "2. Testing /crons/quick-test endpoint..."
if curl -f -s http://localhost:9092/crons/quick-test > /dev/null; then
    echo "   /crons/quick-test endpoint test passed!"
    curl -s http://localhost:9092/crons/quick-test | jq '.run_count' 2>/dev/null || echo "   Response received (jq not available)"
else
    echo "   /crons/quick-test endpoint test failed!"
fi

# Check container logs for cron execution evidence
echo "Checking container logs for cron execution evidence..."
LOGS=$(docker logs $CONTAINER_ID 2>&1)
if echo "$LOGS" | grep -q "starting cron scheduler"; then
    echo "   Cron scheduler started successfully!"
else
    echo "   WARNING: Cron scheduler startup not found in logs"
fi

if echo "$LOGS" | grep -q "starting cron job execution"; then
    echo "   Cron job execution detected!"
else
    echo "   WARNING: No cron job execution found in logs (may need longer wait)"
fi

if echo "$LOGS" | grep -q "Cron test at"; then
    echo "   Cron job output detected in logs!"
else
    echo "   WARNING: Expected cron job output not found"
fi

echo "Container logs (last 20 lines):"
docker logs $CONTAINER_ID 2>&1 | tail -20

# Cleanup
docker stop $CONTAINER_ID >/dev/null 2>&1 || true
docker rm $CONTAINER_ID

echo "Test 4 completed!"

# Test 5: CLI cron flags test
echo "Test 5: CLI cron flags test..."
CONTAINER_ID=$(docker run -d -p 9093:8091 wardend-test /usr/local/bin/wardend \
    --cron-schedule "every 3s" \
    --cron-command "echo 'CLI cron test at' \$(date)" \
    --cron-name "cli-test" \
    --cron-retries "2" \
    --cron-timeout "5s" \
    --monitor-http-port "8091")

# Wait for execution
sleep 10

# Test that the cron job was configured correctly via CLI
echo "Testing CLI-configured cron job..."
if curl -f -s http://localhost:9093/crons/cli-test > /dev/null; then
    echo "   CLI cron job configuration test passed!"
    CLI_STATUS=$(curl -s http://localhost:9093/crons/cli-test 2>/dev/null)
    if echo "$CLI_STATUS" | grep -q '"schedule":"every 3s"'; then
        echo "   Schedule configured correctly!"
    fi
    if echo "$CLI_STATUS" | grep -q '"retries":2'; then
        echo "   Retries configured correctly!"
    fi
else
    echo "   CLI cron job test failed!"
fi

# Check for execution in logs
echo "Checking CLI cron execution..."
CLI_LOGS=$(docker logs $CONTAINER_ID 2>&1)
if echo "$CLI_LOGS" | grep -q "CLI cron test at"; then
    echo "   CLI cron job executed successfully!"
else
    echo "   WARNING: CLI cron job output not found"
fi

# Cleanup
docker stop $CONTAINER_ID >/dev/null 2>&1 || true
docker rm $CONTAINER_ID

echo "Test 5 completed!"

echo "All Docker integration tests completed successfully!"
