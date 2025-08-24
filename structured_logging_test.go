package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestStructuredLogging_ProcessLifecycle(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger that writes to our buffer in JSON format
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.LogLevel = LogLevelDebug

	process := config.AddProcess("test-structured", "echo 'test message'")
	process.RestartPolicy = RestartNever

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	manager.logger = logger // Override with our test logger

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Start and immediately stop process to capture lifecycle logs
	if err := manager.StartProcess("test-structured"); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Give the process time to complete
	time.Sleep(500 * time.Millisecond)

	if err := manager.StopProcess("test-structured"); err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}

	// Parse the log output
	logOutput := buf.String()
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

	// Look for structured fields in the logs
	foundProcessName := false
	foundCommand := false
	foundRestartPolicy := false
	foundPid := false

	for _, line := range logLines {
		if line == "" {
			continue
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			t.Logf("Skipping non-JSON log line: %s", line)
			continue
		}

		// Check for structured fields
		if processName, exists := logEntry["process"]; exists && processName == "test-structured" {
			foundProcessName = true
		}
		if command, exists := logEntry["command"]; exists && strings.Contains(command.(string), "echo") {
			foundCommand = true
		}
		if restartPolicy, exists := logEntry["restart_policy"]; exists && restartPolicy == "never" {
			foundRestartPolicy = true
		}
		if pid, exists := logEntry["pid"]; exists && pid != nil {
			foundPid = true
		}
	}

	if !foundProcessName {
		t.Error("Expected to find 'process' field in structured logs")
	}
	if !foundCommand {
		t.Error("Expected to find 'command' field in structured logs")
	}
	if !foundRestartPolicy {
		t.Error("Expected to find 'restart_policy' field in structured logs")
	}
	if !foundPid {
		t.Error("Expected to find 'pid' field in structured logs")
	}
}

func TestStructuredLogging_RestartAttempts(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger that writes to our buffer in JSON format
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.LogLevel = LogLevelDebug
	config.StartRetries = 2
	config.RestartDelay = 50 * time.Millisecond

	process := config.AddProcess("failing-process", "exit 1")
	process.RestartPolicy = RestartOnFailure
	process.StartRetries = 2
	process.StartupTime = 100 * time.Millisecond

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	manager.logger = logger // Override with our test logger

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Start the failing process
	if err := manager.StartProcess("failing-process"); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for restart attempts to complete
	time.Sleep(1 * time.Second)

	// Parse the log output
	logOutput := buf.String()
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

	// Look for restart-specific structured fields
	foundPhase := false
	foundStartupAttempt := false
	foundMaxRetries := false
	foundExitCode := false

	for _, line := range logLines {
		if line == "" {
			continue
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			continue
		}

		// Check for restart-specific fields
		if phase, exists := logEntry["phase"]; exists && phase == "startup" {
			foundPhase = true
		}
		if startupAttempt, exists := logEntry["startup_attempt"]; exists && startupAttempt != nil {
			foundStartupAttempt = true
		}
		if maxRetries, exists := logEntry["max_retries"]; exists && maxRetries != nil {
			foundMaxRetries = true
		}
		if exitCode, exists := logEntry["exit_code"]; exists && exitCode != nil {
			foundExitCode = true
		}
	}

	if !foundPhase {
		t.Error("Expected to find 'phase' field in restart logs")
	}
	if !foundStartupAttempt {
		t.Error("Expected to find 'startup_attempt' field in restart logs")
	}
	if !foundMaxRetries {
		t.Error("Expected to find 'max_retries' field in restart logs")
	}
	if !foundExitCode {
		t.Error("Expected to find 'exit_code' field in failure logs")
	}
}

func TestStructuredLogging_HealthChecks(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger that writes to our buffer in JSON format
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.LogLevel = LogLevelDebug

	process := config.AddProcess("health-test", "echo 'running'")
	process.HealthCheck = "echo 'healthy'"
	process.HealthInterval = 100 * time.Millisecond

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	manager.logger = logger // Override with our test logger

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Execute a health check directly
	manager.healthChecker.executeHealthCheck("health-test", "echo 'health check passed'")

	// Parse the log output
	logOutput := buf.String()
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

	// Look for health check structured fields
	foundProcess := false
	foundCommand := false
	foundDuration := false

	for _, line := range logLines {
		if line == "" {
			continue
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			continue
		}

		// Look for health check logs
		if msg, exists := logEntry["msg"]; exists && strings.Contains(msg.(string), "health check") {
			if process, exists := logEntry["process"]; exists && process == "health-test" {
				foundProcess = true
			}
			if command, exists := logEntry["command"]; exists && command != nil {
				foundCommand = true
			}
			if duration, exists := logEntry["duration"]; exists && duration != nil {
				foundDuration = true
			}
		}
	}

	if !foundProcess {
		t.Error("Expected to find 'process' field in health check logs")
	}
	if !foundCommand {
		t.Error("Expected to find 'command' field in health check logs")
	}
	if !foundDuration {
		t.Error("Expected to find 'duration' field in health check logs")
	}
}

func TestStructuredLogging_DependencyWaiting(t *testing.T) {
	var buf bytes.Buffer

	// Create a logger that writes to our buffer in JSON format
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.LogLevel = LogLevelDebug

	// Create dependency process that stays running longer
	dependency := config.AddProcess("dependency", "sleep 5")
	dependency.RestartPolicy = RestartNever

	dependent := config.AddProcess("dependent", "echo 'started'")
	dependent.DependsOn = []string{"dependency"}
	dependent.RestartPolicy = RestartNever

	if err := config.Validate(); err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	manager := NewManager(config)
	manager.logger = logger // Override with our test logger

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Manager initialization failed: %v", err)
	}

	// Start the dependency process first
	if err := manager.StartProcess("dependency"); err != nil {
		t.Fatalf("Failed to start dependency process: %v", err)
	}

	// Wait for dependency to be running
	timeout := time.Now().Add(2 * time.Second)
	for time.Now().Before(timeout) {
		state, _ := manager.GetProcessState("dependency")
		if state == StateRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Now call waitForDependencies - should succeed quickly since dependency is running
	err := manager.waitForDependencies("dependent")

	// Parse the log output
	logOutput := buf.String()
	t.Logf("Log output: %s", logOutput)

	// Should succeed since dependency is running
	if err != nil {
		t.Errorf("Expected waitForDependencies to succeed, got error: %v", err)
	}

	// Look for structured logging fields that show we've improved the logging
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")

	foundStructuredField := false
	foundDependencyField := false

	for _, line := range logLines {
		if line == "" {
			continue
		}

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			continue
		}

		// Look for any structured fields beyond just message
		if len(logEntry) > 3 { // msg, time, level + others
			foundStructuredField = true
		}

		// Look specifically for dependency-related fields
		if _, exists := logEntry["dependency"]; exists {
			foundDependencyField = true
		}
		if _, exists := logEntry["dependencies"]; exists {
			foundDependencyField = true
		}
	}

	if !foundStructuredField {
		t.Error("Expected to find structured fields in logs")
	}
	if !foundDependencyField {
		t.Error("Expected to find dependency-related fields in logs")
	}

	// Clean up
	manager.cancel()
}
