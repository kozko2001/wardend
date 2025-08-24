package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPServer_HealthEndpoint(t *testing.T) {
	config := NewConfig()

	// Add a process with health check
	process := config.AddProcess("web", "echo 'running'")
	process.HealthCheck = "echo 'healthy'"
	process.HealthInterval = 1 * time.Second

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Create HTTP server
	httpServer := NewHTTPServer(manager, 8080)

	// Execute health check to have some data
	manager.healthChecker.executeHealthCheck("web", "echo 'healthy'")

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	httpServer.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", resp.Header.Get("Content-Type"))
	}

	var response HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	if response.Summary == nil {
		t.Error("Expected summary to be present")
	}

	if response.Timestamp == "" {
		t.Error("Expected timestamp to be present")
	}
}

func TestHTTPServer_HealthEndpoint_Unhealthy(t *testing.T) {
	config := NewConfig()

	// Add a process with failing health check
	process := config.AddProcess("failing", "echo 'running'")
	process.HealthCheck = "exit 1"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Execute failing health check
	manager.healthChecker.executeHealthCheck("failing", "exit 1")

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	httpServer.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	var response HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Status != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got '%s'", response.Status)
	}
}

func TestHTTPServer_StatusEndpoint(t *testing.T) {
	config := NewConfig()

	// Add multiple processes
	config.AddProcess("web", "nginx")
	worker := config.AddProcess("worker", "python worker.py")
	worker.DependsOn = []string{"web"}

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Test status endpoint
	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	httpServer.handleStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response.Processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(response.Processes))
	}

	if webProcess, exists := response.Processes["web"]; exists {
		if webProcess.Command != "nginx" {
			t.Errorf("Expected web command 'nginx', got '%s'", webProcess.Command)
		}
		if webProcess.RestartPolicy != "always" {
			t.Errorf("Expected web restart policy 'always', got '%s'", webProcess.RestartPolicy)
		}
	} else {
		t.Error("Expected 'web' process in response")
	}

	if workerProcess, exists := response.Processes["worker"]; exists {
		if len(workerProcess.DependsOn) != 1 || workerProcess.DependsOn[0] != "web" {
			t.Errorf("Expected worker to depend on 'web', got %v", workerProcess.DependsOn)
		}
	} else {
		t.Error("Expected 'worker' process in response")
	}
}

func TestHTTPServer_ProcessesEndpoint(t *testing.T) {
	config := NewConfig()

	config.AddProcess("app1", "echo app1")
	config.AddProcess("app2", "echo app2")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Test processes endpoint
	req := httptest.NewRequest("GET", "/processes", nil)
	w := httptest.NewRecorder()

	httpServer.handleProcesses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if count, exists := response["count"]; !exists || count != float64(2) {
		t.Errorf("Expected count 2, got %v", count)
	}

	if processes, exists := response["processes"]; exists {
		processesSlice := processes.([]interface{})
		if len(processesSlice) != 2 {
			t.Errorf("Expected 2 processes, got %d", len(processesSlice))
		}
	} else {
		t.Error("Expected 'processes' field in response")
	}
}

func TestHTTPServer_ProcessEndpoint(t *testing.T) {
	config := NewConfig()

	process := config.AddProcess("test-app", "echo test")
	process.RestartPolicy = RestartOnFailure
	process.HealthCheck = "echo healthy"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Test individual process endpoint
	req := httptest.NewRequest("GET", "/processes/test-app", nil)
	w := httptest.NewRecorder()

	httpServer.handleProcess(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if name, exists := response["name"]; !exists || name != "test-app" {
		t.Errorf("Expected name 'test-app', got %v", name)
	}

	if command, exists := response["command"]; !exists || command != "echo test" {
		t.Errorf("Expected command 'echo test', got %v", command)
	}

	if restartPolicy, exists := response["restart_policy"]; !exists || restartPolicy != "on-failure" {
		t.Errorf("Expected restart_policy 'on-failure', got %v", restartPolicy)
	}

	if healthCheck, exists := response["health_check"]; !exists || healthCheck != "echo healthy" {
		t.Errorf("Expected health_check 'echo healthy', got %v", healthCheck)
	}
}

func TestHTTPServer_ProcessEndpoint_NotFound(t *testing.T) {
	config := NewConfig()
	config.AddProcess("existing", "echo test")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Test non-existent process
	req := httptest.NewRequest("GET", "/processes/non-existent", nil)
	w := httptest.NewRecorder()

	httpServer.handleProcess(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestHTTPServer_MethodNotAllowed(t *testing.T) {
	config := NewConfig()
	config.AddProcess("test", "echo test")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	// Test POST to GET-only endpoint
	req := httptest.NewRequest("POST", "/health", nil)
	w := httptest.NewRecorder()

	httpServer.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHTTPServer_Integration(t *testing.T) {
	config := NewConfig()
	config.HTTPPort = 0 // Let the test pick an available port

	process := config.AddProcess("integration-test", "echo 'integration test'")
	process.HealthCheck = "echo 'integration healthy'"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Test that HTTP server is not started when port is 0
	if manager.httpServer != nil {
		t.Error("Expected HTTP server to be nil when port is 0")
	}

	// Test with a valid port
	config.HTTPPort = 8081
	manager2 := NewManager(config)
	if err := manager2.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Test that HTTP server is created when port is > 0
	if manager2.httpServer == nil {
		t.Error("Expected HTTP server to be created when port > 0")
	}
}

func TestNewHTTPServer(t *testing.T) {
	config := NewConfig()
	config.AddProcess("test", "echo test")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	httpServer := NewHTTPServer(manager, 8080)

	if httpServer == nil {
		t.Fatal("Expected HTTP server to be created")
	}

	if httpServer.server == nil {
		t.Error("Expected HTTP server.server to be initialized")
	}

	if httpServer.manager != manager {
		t.Error("Expected HTTP server.manager to be set correctly")
	}

	if httpServer.server.Addr != ":8080" {
		t.Errorf("Expected server address ':8080', got '%s'", httpServer.server.Addr)
	}
}

func TestHTTPServer_StartStop(t *testing.T) {
	config := NewConfig()
	config.AddProcess("test", "echo test")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Use port 0 to let the OS pick an available port for testing
	httpServer := NewHTTPServer(manager, 0)

	// Note: We can't easily test the actual start/stop without race conditions
	// since the server runs in a goroutine. This test just ensures the methods
	// don't panic and return appropriate values.

	if err := httpServer.Start(); err != nil {
		t.Errorf("Expected Start() to succeed, got error: %v", err)
	}

	// Give the server a moment to start
	time.Sleep(10 * time.Millisecond)

	if err := httpServer.Stop(); err != nil {
		t.Errorf("Expected Stop() to succeed, got error: %v", err)
	}
}
