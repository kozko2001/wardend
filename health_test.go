package main

import (
	"testing"
	"time"
)

func TestHealthChecker_BasicFunctionality(t *testing.T) {
	config := NewConfig()

	// Add a process with health check
	process := config.AddProcess("test-healthy", "echo 'healthy'")
	process.HealthCheck = "echo 'health check passed'"
	process.HealthInterval = 100 * time.Millisecond

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Test health checker initialization
	if manager.healthChecker == nil {
		t.Fatal("Health checker should be initialized")
	}

	// Start health checker (but don't start processes since we just want to test health check logic)
	manager.healthChecker.Start()
	defer manager.healthChecker.Stop()

	// Execute health check directly
	manager.healthChecker.executeHealthCheck("test-healthy", "echo 'test passed'")

	// Get health status
	result := manager.GetHealthStatus("test-healthy")
	if result == nil {
		t.Fatal("Health status should not be nil")
	}

	if result.Status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got: %v", result.Status)
	}

	if result.Output != "test passed\n" {
		t.Errorf("Expected 'test passed\\n', got: %s", result.Output)
	}
}

func TestHealthChecker_FailedHealthCheck(t *testing.T) {
	config := NewConfig()

	// Add a process with failing health check
	process := config.AddProcess("test-unhealthy", "echo 'running'")
	process.HealthCheck = "exit 1"
	process.HealthInterval = 100 * time.Millisecond

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Execute failing health check directly
	manager.healthChecker.executeHealthCheck("test-unhealthy", "exit 1")

	// Get health status
	result := manager.GetHealthStatus("test-unhealthy")
	if result == nil {
		t.Fatal("Health status should not be nil")
	}

	if result.Status != HealthStatusUnhealthy {
		t.Errorf("Expected unhealthy status, got: %v", result.Status)
	}

	if result.Error == nil {
		t.Error("Expected error to be set for failed health check")
	}
}

func TestHealthChecker_OverallHealth(t *testing.T) {
	config := NewConfig()

	// Add multiple processes with different health check outcomes
	process1 := config.AddProcess("test-healthy-1", "echo 'service1'")
	process1.HealthCheck = "echo 'service1 ok'"

	process2 := config.AddProcess("test-healthy-2", "echo 'service2'")
	process2.HealthCheck = "echo 'service2 ok'"

	process3 := config.AddProcess("test-unhealthy", "echo 'service3'")
	process3.HealthCheck = "exit 1"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Execute health checks directly
	manager.healthChecker.executeHealthCheck("test-healthy-1", "echo 'service1 ok'")
	manager.healthChecker.executeHealthCheck("test-healthy-2", "echo 'service2 ok'")
	manager.healthChecker.executeHealthCheck("test-unhealthy", "exit 1")

	// Check overall health (should be false due to one unhealthy service)
	if manager.IsOverallHealthy() {
		t.Error("Overall health should be false when one service is unhealthy")
	}

	// Get health summary
	summary := manager.GetHealthSummary()

	if summary["overall_status"] != "unhealthy" {
		t.Errorf("Expected overall_status to be 'unhealthy', got: %v", summary["overall_status"])
	}

	if summary["total_checks"] != 3 {
		t.Errorf("Expected 3 total checks, got: %v", summary["total_checks"])
	}

	if summary["healthy_count"] != 2 {
		t.Errorf("Expected 2 healthy checks, got: %v", summary["healthy_count"])
	}

	if summary["unhealthy_count"] != 1 {
		t.Errorf("Expected 1 unhealthy check, got: %v", summary["unhealthy_count"])
	}
}

func TestHealthChecker_AllHealthy(t *testing.T) {
	config := NewConfig()

	// Add multiple healthy processes
	process1 := config.AddProcess("test-healthy-1", "echo 'service1'")
	process1.HealthCheck = "echo 'service1 ok'"

	process2 := config.AddProcess("test-healthy-2", "echo 'service2'")
	process2.HealthCheck = "echo 'service2 ok'"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Execute health checks directly
	manager.healthChecker.executeHealthCheck("test-healthy-1", "echo 'service1 ok'")
	manager.healthChecker.executeHealthCheck("test-healthy-2", "echo 'service2 ok'")

	// Check overall health (should be true)
	if !manager.IsOverallHealthy() {
		t.Error("Overall health should be true when all services are healthy")
	}

	// Get health summary
	summary := manager.GetHealthSummary()

	if summary["overall_status"] != "healthy" {
		t.Errorf("Expected overall_status to be 'healthy', got: %v", summary["overall_status"])
	}
}

func TestHealthChecker_UnknownProcess(t *testing.T) {
	config := NewConfig()
	config.AddProcess("test-process", "echo 'test'")

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Get health status for unknown process
	result := manager.GetHealthStatus("unknown-process")
	if result == nil {
		t.Fatal("Health status should not be nil")
	}

	if result.Status != HealthStatusUnknown {
		t.Errorf("Expected unknown status, got: %v", result.Status)
	}
}

func TestHealthChecker_GetAllHealthStatuses(t *testing.T) {
	config := NewConfig()

	process1 := config.AddProcess("test-1", "echo 'service1'")
	process1.HealthCheck = "echo 'ok1'"

	process2 := config.AddProcess("test-2", "echo 'service2'")
	process2.HealthCheck = "echo 'ok2'"

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Execute health checks
	manager.healthChecker.executeHealthCheck("test-1", "echo 'ok1'")
	manager.healthChecker.executeHealthCheck("test-2", "echo 'ok2'")

	// Get all health statuses
	allStatuses := manager.GetAllHealthStatuses()

	if len(allStatuses) != 2 {
		t.Errorf("Expected 2 health statuses, got: %d", len(allStatuses))
	}

	if allStatuses["test-1"] == nil || allStatuses["test-1"].Status != HealthStatusHealthy {
		t.Error("test-1 should be healthy")
	}

	if allStatuses["test-2"] == nil || allStatuses["test-2"].Status != HealthStatusHealthy {
		t.Error("test-2 should be healthy")
	}
}
