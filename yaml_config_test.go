package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadYAMLConfig_BasicConfig(t *testing.T) {
	yamlContent := `
processes:
  - name: web
    command: "nginx -g 'daemon off;'"
    restart_policy: always
    health_check: "curl -f http://localhost/health"
    health_interval: "30s"
  - name: worker
    command: "python worker.py"
    restart_policy: on-failure
    depends_on: ["web"]
    start_retries: 5
    startup_time: "120s"
    max_restarts: "10"
    restart_delay: "2s"

log_format: json
log_level: debug
log_dir: "/var/log/wardend"
shutdown_timeout: "30s"
restart_delay: "2s"
start_retries: 3
startup_time: "60s"
max_restarts: "infinite"
health_interval: "45s"
`

	// Create temporary YAML file
	tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlPath := filepath.Join(tmpDir, "test-config.yml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	// Load configuration
	config, err := LoadYAMLConfig(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Validate global settings
	if config.LogFormat != LogFormatJSON {
		t.Errorf("Expected LogFormat JSON, got %v", config.LogFormat)
	}

	if config.LogLevel != LogLevelDebug {
		t.Errorf("Expected LogLevel debug, got %v", config.LogLevel)
	}

	if config.LogDir != "/var/log/wardend" {
		t.Errorf("Expected LogDir '/var/log/wardend', got %s", config.LogDir)
	}

	if config.ShutdownTimeout != 30*time.Second {
		t.Errorf("Expected ShutdownTimeout 30s, got %v", config.ShutdownTimeout)
	}

	if config.RestartDelay != 2*time.Second {
		t.Errorf("Expected RestartDelay 2s, got %v", config.RestartDelay)
	}

	if config.StartRetries != 3 {
		t.Errorf("Expected StartRetries 3, got %d", config.StartRetries)
	}

	if config.StartupTime != 60*time.Second {
		t.Errorf("Expected StartupTime 60s, got %v", config.StartupTime)
	}

	if config.MaxRestarts != -1 {
		t.Errorf("Expected MaxRestarts -1 (infinite), got %d", config.MaxRestarts)
	}

	if config.HealthInterval != 45*time.Second {
		t.Errorf("Expected HealthInterval 45s, got %v", config.HealthInterval)
	}

	// Validate processes
	if len(config.Processes) != 2 {
		t.Fatalf("Expected 2 processes, got %d", len(config.Processes))
	}

	// Check web process
	webProcess := config.Processes[0]
	if webProcess.Name != "web" {
		t.Errorf("Expected web process name 'web', got %s", webProcess.Name)
	}
	if webProcess.Command != "nginx -g 'daemon off;'" {
		t.Errorf("Expected web command 'nginx -g 'daemon off;'', got %s", webProcess.Command)
	}
	if webProcess.RestartPolicy != RestartAlways {
		t.Errorf("Expected web restart policy 'always', got %v", webProcess.RestartPolicy)
	}
	if webProcess.HealthCheck != "curl -f http://localhost/health" {
		t.Errorf("Expected web health check 'curl -f http://localhost/health', got %s", webProcess.HealthCheck)
	}
	if webProcess.HealthInterval != 30*time.Second {
		t.Errorf("Expected web health interval 30s, got %v", webProcess.HealthInterval)
	}

	// Check worker process
	workerProcess := config.Processes[1]
	if workerProcess.Name != "worker" {
		t.Errorf("Expected worker process name 'worker', got %s", workerProcess.Name)
	}
	if workerProcess.Command != "python worker.py" {
		t.Errorf("Expected worker command 'python worker.py', got %s", workerProcess.Command)
	}
	if workerProcess.RestartPolicy != RestartOnFailure {
		t.Errorf("Expected worker restart policy 'on-failure', got %v", workerProcess.RestartPolicy)
	}
	if len(workerProcess.DependsOn) != 1 || workerProcess.DependsOn[0] != "web" {
		t.Errorf("Expected worker to depend on 'web', got %v", workerProcess.DependsOn)
	}
	if workerProcess.StartRetries != 5 {
		t.Errorf("Expected worker start retries 5, got %d", workerProcess.StartRetries)
	}
	if workerProcess.StartupTime != 120*time.Second {
		t.Errorf("Expected worker startup time 120s, got %v", workerProcess.StartupTime)
	}
	if workerProcess.MaxRestarts != 10 {
		t.Errorf("Expected worker max restarts 10, got %d", workerProcess.MaxRestarts)
	}
	if workerProcess.RestartDelay != 2*time.Second {
		t.Errorf("Expected worker restart delay 2s, got %v", workerProcess.RestartDelay)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		t.Errorf("Configuration validation failed: %v", err)
	}
}

func TestLoadYAMLConfig_MinimalConfig(t *testing.T) {
	yamlContent := `
processes:
  - name: simple
    command: "echo hello"
`

	// Create temporary YAML file
	tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlPath := filepath.Join(tmpDir, "minimal-config.yml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	// Load configuration
	config, err := LoadYAMLConfig(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Check defaults are applied
	if config.LogFormat != LogFormatText {
		t.Errorf("Expected default LogFormat text, got %v", config.LogFormat)
	}

	if config.LogLevel != LogLevelInfo {
		t.Errorf("Expected default LogLevel info, got %v", config.LogLevel)
	}

	if config.ShutdownTimeout != 10*time.Second {
		t.Errorf("Expected default ShutdownTimeout 10s, got %v", config.ShutdownTimeout)
	}

	if config.MaxRestarts != -1 {
		t.Errorf("Expected default MaxRestarts -1 (infinite), got %d", config.MaxRestarts)
	}

	// Check process
	if len(config.Processes) != 1 {
		t.Fatalf("Expected 1 process, got %d", len(config.Processes))
	}

	process := config.Processes[0]
	if process.Name != "simple" {
		t.Errorf("Expected process name 'simple', got %s", process.Name)
	}
	if process.Command != "echo hello" {
		t.Errorf("Expected command 'echo hello', got %s", process.Command)
	}
	if process.RestartPolicy != RestartAlways {
		t.Errorf("Expected default restart policy 'always', got %v", process.RestartPolicy)
	}
}

func TestLoadYAMLConfig_InvalidFile(t *testing.T) {
	// Test non-existent file
	_, err := LoadYAMLConfig("non-existent.yml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test invalid YAML
	yamlContent := `
processes:
  - name: test
    command: echo test
  invalid_yaml: [
`

	tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlPath := filepath.Join(tmpDir, "invalid.yml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	_, err = LoadYAMLConfig(yamlPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadYAMLConfig_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name        string
		yamlContent string
		expectedErr string
	}{
		{
			name: "missing process name",
			yamlContent: `
processes:
  - command: "echo test"
`,
			expectedErr: "process name cannot be empty",
		},
		{
			name: "missing process command",
			yamlContent: `
processes:
  - name: test
`,
			expectedErr: "process command cannot be empty",
		},
		{
			name: "invalid log format",
			yamlContent: `
log_format: invalid
processes:
  - name: test
    command: "echo test"
`,
			expectedErr: "invalid log_format",
		},
		{
			name: "invalid restart policy",
			yamlContent: `
processes:
  - name: test
    command: "echo test"
    restart_policy: invalid
`,
			expectedErr: "invalid restart_policy",
		},
		{
			name: "invalid duration",
			yamlContent: `
shutdown_timeout: invalid
processes:
  - name: test
    command: "echo test"
`,
			expectedErr: "invalid shutdown_timeout",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			yamlPath := filepath.Join(tmpDir, "test.yml")
			if err := os.WriteFile(yamlPath, []byte(tc.yamlContent), 0o644); err != nil {
				t.Fatalf("Failed to write YAML file: %v", err)
			}

			_, err = LoadYAMLConfig(yamlPath)
			if err == nil {
				t.Errorf("Expected error containing '%s'", tc.expectedErr)
			} else if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("Expected error containing '%s', got '%v'", tc.expectedErr, err)
			}
		})
	}
}

func TestSaveYAMLConfig(t *testing.T) {
	// Create a config
	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.LogLevel = LogLevelDebug
	config.LogDir = "/var/log"
	config.ShutdownTimeout = 30 * time.Second
	config.RestartDelay = 2 * time.Second
	config.StartRetries = 5
	config.StartupTime = 120 * time.Second
	config.MaxRestarts = 10
	config.HealthInterval = 45 * time.Second

	webProcess := config.AddProcess("web", "nginx -g 'daemon off;'")
	webProcess.RestartPolicy = RestartAlways
	webProcess.HealthCheck = "curl -f http://localhost/health"
	webProcess.HealthInterval = 30 * time.Second

	workerProcess := config.AddProcess("worker", "python worker.py")
	workerProcess.RestartPolicy = RestartOnFailure
	workerProcess.DependsOn = []string{"web"}
	workerProcess.StartRetries = 3
	workerProcess.MaxRestarts = -1

	// Save to temporary file
	tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlPath := filepath.Join(tmpDir, "saved-config.yml")
	if err := SaveYAMLConfig(config, yamlPath); err != nil {
		t.Fatalf("Failed to save YAML config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		t.Fatal("YAML config file was not created")
	}

	// Load it back and verify
	loadedConfig, err := LoadYAMLConfig(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load saved YAML config: %v", err)
	}

	// Check some key values
	if loadedConfig.LogFormat != config.LogFormat {
		t.Errorf("LogFormat mismatch: expected %v, got %v", config.LogFormat, loadedConfig.LogFormat)
	}

	if len(loadedConfig.Processes) != len(config.Processes) {
		t.Errorf("Process count mismatch: expected %d, got %d", len(config.Processes), len(loadedConfig.Processes))
	}

	// Check process details
	if len(loadedConfig.Processes) > 0 {
		webProc := loadedConfig.Processes[0]
		if webProc.Name != "web" {
			t.Errorf("Web process name mismatch: expected 'web', got %s", webProc.Name)
		}
		if webProc.HealthCheck != "curl -f http://localhost/health" {
			t.Errorf("Web health check mismatch: expected 'curl -f http://localhost/health', got %s", webProc.HealthCheck)
		}
	}
}

func TestYAMLConfig_ComplexDependencies(t *testing.T) {
	yamlContent := `
processes:
  - name: database
    command: "postgres"
    restart_policy: always
    
  - name: cache
    command: "redis-server"
    restart_policy: always
    depends_on: ["database"]
    
  - name: api
    command: "python api.py"
    restart_policy: on-failure
    depends_on: ["database", "cache"]
    
  - name: worker
    command: "python worker.py"
    restart_policy: on-failure
    depends_on: ["api"]
    
  - name: nginx
    command: "nginx -g 'daemon off;'"
    restart_policy: always
    depends_on: ["api"]
    health_check: "curl -f http://localhost/health"
`

	tmpDir, err := os.MkdirTemp("", "wardend-yaml-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlPath := filepath.Join(tmpDir, "complex-deps.yml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	config, err := LoadYAMLConfig(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Validate configuration (should pass dependency validation)
	if err := config.Validate(); err != nil {
		t.Errorf("Configuration validation failed: %v", err)
	}

	// Check specific dependencies
	processMap := make(map[string]ProcessConfig)
	for _, proc := range config.Processes {
		processMap[proc.Name] = proc
	}

	if cache, exists := processMap["cache"]; exists {
		if len(cache.DependsOn) != 1 || cache.DependsOn[0] != "database" {
			t.Errorf("Cache should depend on database, got %v", cache.DependsOn)
		}
	} else {
		t.Error("Cache process not found")
	}

	if api, exists := processMap["api"]; exists {
		expectedDeps := []string{"database", "cache"}
		if len(api.DependsOn) != 2 {
			t.Errorf("API should have 2 dependencies, got %d", len(api.DependsOn))
		}
		for _, expectedDep := range expectedDeps {
			found := false
			for _, actualDep := range api.DependsOn {
				if actualDep == expectedDep {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("API should depend on %s", expectedDep)
			}
		}
	} else {
		t.Error("API process not found")
	}
}

func TestFromConfig(t *testing.T) {
	// Create a config with various settings
	config := NewConfig()
	config.LogFormat = LogFormatJSON
	config.MaxRestarts = -1

	process := config.AddProcess("test", "echo hello")
	process.MaxRestarts = 5

	// Convert to YAML config
	yamlConfig := FromConfig(config)

	// Check conversion
	if yamlConfig.LogFormat != "json" {
		t.Errorf("Expected LogFormat 'json', got %s", yamlConfig.LogFormat)
	}

	if yamlConfig.MaxRestarts != "infinite" {
		t.Errorf("Expected MaxRestarts 'infinite', got %s", yamlConfig.MaxRestarts)
	}

	if len(yamlConfig.Processes) != 1 {
		t.Fatalf("Expected 1 process, got %d", len(yamlConfig.Processes))
	}

	yamlProcess := yamlConfig.Processes[0]
	if yamlProcess.Name != "test" {
		t.Errorf("Expected process name 'test', got %s", yamlProcess.Name)
	}

	if yamlProcess.MaxRestarts != "5" {
		t.Errorf("Expected process MaxRestarts '5', got %s", yamlProcess.MaxRestarts)
	}
}
