package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestDockerAvailable checks if Docker is available for testing
func TestDockerAvailable(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping all Docker tests")
	}
	
	t.Log("Docker is available for testing")
}

// TestDockerBuild tests building the Docker image
func TestDockerBuild(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping Docker build test")
	}

	t.Log("Building Docker test image...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "docker", "build", "-f", "Dockerfile.test", "-t", "wardend-test", ".")
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		t.Fatalf("Failed to build Docker image: %v\nOutput: %s", err, string(output))
	}
	
	t.Log("Docker image built successfully")
}

// TestDockerBasicRun tests basic wardend functionality in Docker
func TestDockerBasicRun(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	// Ensure image is built
	if err := buildDockerImage(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Run a simple wardend command
	containerID, err := runDockerContainer([]string{
		"/usr/local/bin/wardend",
		"--run", "echo 'Docker test successful'",
		"--name", "docker-test",
		"--restart", "never",
		"--log-format", "json",
	}, nil)
	
	if err != nil {
		t.Fatalf("Failed to run container: %v", err)
	}
	defer cleanupContainer(containerID)

	// Wait for the process to run (wardend will keep running to monitor)
	time.Sleep(3 * time.Second)
	
	// Stop the container manually since wardend stays running for monitoring
	stopCmd := exec.Command("docker", "stop", containerID)
	if err := stopCmd.Run(); err != nil {
		t.Logf("Failed to stop container (may be expected): %v", err)
	}

	// Get and verify logs
	logs, err := getContainerLogs(containerID)
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Container logs:\n%s", logs)

	// Verify expected content
	if !strings.Contains(logs, "Docker test successful") {
		t.Error("Expected test message not found in logs")
	}

	// Verify JSON logging
	if !strings.Contains(logs, `"msg"`) || !strings.Contains(logs, `"level"`) {
		t.Error("Expected JSON formatted logs")
	}
}

// TestDockerHealthEndpoint tests the HTTP health endpoint in Docker
func TestDockerHealthEndpoint(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	// Ensure image is built
	if err := buildDockerImage(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Run wardend with health endpoint using a different port
	containerID, err := runDockerContainer([]string{
		"/usr/local/bin/wardend",
		"--run", "sleep 30",
		"--name", "health-test",
		"--restart", "never",
		"--monitor-http-port", "9091",
		"--health-check", "echo 'healthy'",
		"--health-interval", "2s",
	}, map[string]string{
		"9091": "9091",
	})
	
	if err != nil {
		t.Fatalf("Failed to run container: %v", err)
	}
	defer cleanupContainer(containerID)

	// Wait for startup
	time.Sleep(3 * time.Second)

	// Test health endpoint
	resp, err := http.Get("http://localhost:9091/health")
	if err != nil {
		t.Fatalf("Failed to connect to health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Decode response
	var healthResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Errorf("Failed to decode health response: %v", err)
	} else {
		t.Logf("Health response: %+v", healthResp)
		
		// Verify response structure
		if status, ok := healthResp["status"].(string); !ok || status == "" {
			t.Error("Health response missing or invalid status field")
		}
		
		if summary, ok := healthResp["summary"].(map[string]interface{}); !ok || len(summary) == 0 {
			t.Error("Health response missing or invalid summary field")
		}
	}
}

// TestDockerYAMLConfig tests YAML configuration loading in Docker
func TestDockerYAMLConfig(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	// Ensure image is built
	if err := buildDockerImage(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Run wardend with YAML config
	containerID, err := runDockerContainer([]string{
		"/usr/local/bin/wardend",
		"--config", "/app/test/basic-test.yml",
	}, nil)
	
	if err != nil {
		t.Fatalf("Failed to run container: %v", err)
	}
	defer cleanupContainer(containerID)

	// Wait for processes to complete
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	
	if err := waitForContainer(ctx, containerID); err != nil {
		t.Logf("Container still running (may be expected): %v", err)
		// Don't fail here, container might be running health checks
	}

	// Get and verify logs
	logs, err := getContainerLogs(containerID)
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Container logs:\n%s", logs)

	// Verify YAML config was loaded
	if !strings.Contains(logs, "Web server starting") {
		t.Error("Expected web process from YAML config")
	}
	
	if !strings.Contains(logs, "Worker starting") {
		t.Error("Expected worker process from YAML config") 
	}

	// Verify processes started in correct dependency order (web before worker)
	webStartPos := strings.Index(logs, `"process":"web"`)
	workerStartPos := strings.Index(logs, `"process":"worker"`)
	if webStartPos == -1 || workerStartPos == -1 || webStartPos > workerStartPos {
		t.Error("Expected web process to start before worker process (dependency order)")
	}

	// Verify JSON logging (from YAML config)
	if !strings.Contains(logs, `"level"`) {
		t.Error("Expected JSON formatted logs from YAML config")
	}
}

// TestDockerSignalHandling tests graceful shutdown via signals
func TestDockerSignalHandling(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	// Ensure image is built
	if err := buildDockerImage(); err != nil {
		t.Fatalf("Failed to build Docker image: %v", err)
	}

	// Run long-running wardend process
	containerID, err := runDockerContainer([]string{
		"/usr/local/bin/wardend",
		"--run", "sleep 60",
		"--name", "signal-test",
		"--restart", "never",
		"--shutdown-timeout", "5s",
		"--log-format", "json",
	}, nil)
	
	if err != nil {
		t.Fatalf("Failed to run container: %v", err)
	}
	defer cleanupContainer(containerID)

	// Wait for process to start
	time.Sleep(2 * time.Second)

	// Send SIGTERM to the container
	cmd := exec.Command("docker", "stop", "-t", "10", containerID)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to stop container: %v", err)
	}

	// Wait for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	
	if err := waitForContainer(ctx, containerID); err != nil {
		t.Errorf("Container did not shut down gracefully: %v", err)
	}

	// Get logs to verify graceful shutdown
	logs, err := getContainerLogs(containerID)
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Shutdown logs:\n%s", logs)

	// Verify graceful shutdown occurred
	if !strings.Contains(logs, "stopping process") {
		t.Error("Expected graceful process stopping in logs")
	}
}

// Helper functions for Docker operations

func buildDockerImage() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "docker", "build", "-f", "Dockerfile.test", "-t", "wardend-test", ".")
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("failed to build Docker image: %v\nOutput: %s", err, string(output))
	}
	
	return nil
}

func runDockerContainer(args []string, ports map[string]string) (string, error) {
	dockerArgs := []string{"run", "-d"}
	
	// Add port mappings
	if ports != nil {
		for hostPort, containerPort := range ports {
			dockerArgs = append(dockerArgs, "-p", fmt.Sprintf("%s:%s", hostPort, containerPort))
		}
	}
	
	// Add the image name
	dockerArgs = append(dockerArgs, "wardend-test")
	
	// Add wardend arguments
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run Docker container: %v\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

func waitForContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "wait", containerID)
	return cmd.Run()
}

func getContainerLogs(containerID string) (string, error) {
	cmd := exec.Command("docker", "logs", containerID)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %v", err)
	}
	return string(output), nil
}

func cleanupContainer(containerID string) {
	// Stop the container
	stopCmd := exec.Command("docker", "stop", containerID)
	_ = stopCmd.Run()
	
	// Remove the container
	rmCmd := exec.Command("docker", "rm", containerID)
	_ = rmCmd.Run()
}

func isDockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}

// TestDockerIntegrationSuite runs the full integration test suite
func TestDockerIntegrationSuite(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping integration test suite")
	}

	// List of all Docker integration tests to run
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"DockerBuild", TestDockerBuild},
		{"DockerBasicRun", TestDockerBasicRun},
		{"DockerHealthEndpoint", TestDockerHealthEndpoint},
		{"DockerYAMLConfig", TestDockerYAMLConfig},
		{"DockerSignalHandling", TestDockerSignalHandling},
	}

	for _, test := range tests {
		t.Run(test.name, test.fn)
	}
}