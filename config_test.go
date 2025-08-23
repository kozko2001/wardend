package main

import (
	"strings"
	"testing"
	"time"
)

func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config == nil {
		t.Fatal("NewConfig() returned nil")
	}

	if len(config.Processes) != 0 {
		t.Errorf("Expected 0 processes, got %d", len(config.Processes))
	}

	if config.LogFormat != LogFormatText {
		t.Errorf("Expected default log format to be text, got %s", config.LogFormat)
	}

	if config.LogLevel != LogLevelInfo {
		t.Errorf("Expected default log level to be info, got %s", config.LogLevel)
	}

	if config.ShutdownTimeout != 10*time.Second {
		t.Errorf("Expected default shutdown timeout to be 10s, got %s", config.ShutdownTimeout)
	}

	if config.RestartDelay != 1*time.Second {
		t.Errorf("Expected default restart delay to be 1s, got %s", config.RestartDelay)
	}

	if config.MaxRestarts != 5 {
		t.Errorf("Expected default max restarts to be 5, got %d", config.MaxRestarts)
	}

	if config.HealthInterval != 30*time.Second {
		t.Errorf("Expected default health interval to be 30s, got %s", config.HealthInterval)
	}
}

func TestConfigAddProcess(t *testing.T) {
	config := NewConfig()
	
	process := config.AddProcess("test", "echo hello")
	
	if len(config.Processes) != 1 {
		t.Errorf("Expected 1 process, got %d", len(config.Processes))
	}
	
	if process.Name != "test" {
		t.Errorf("Expected process name to be 'test', got '%s'", process.Name)
	}
	
	if process.Command != "echo hello" {
		t.Errorf("Expected process command to be 'echo hello', got '%s'", process.Command)
	}
	
	if process.RestartPolicy != RestartAlways {
		t.Errorf("Expected default restart policy to be 'always', got '%s'", process.RestartPolicy)
	}
	
	if len(process.DependsOn) != 0 {
		t.Errorf("Expected no dependencies, got %d", len(process.DependsOn))
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Config)
		wantErr string
	}{
		{
			name:    "no processes",
			setup:   func(c *Config) {},
			wantErr: "no processes configured",
		},
		{
			name: "empty process name",
			setup: func(c *Config) {
				c.Processes = append(c.Processes, ProcessConfig{
					Name:    "",
					Command: "echo test",
				})
			},
			wantErr: "process name cannot be empty",
		},
		{
			name: "empty process command",
			setup: func(c *Config) {
				c.Processes = append(c.Processes, ProcessConfig{
					Name:    "test",
					Command: "",
				})
			},
			wantErr: "command cannot be empty for process 'test'",
		},
		{
			name: "duplicate process name",
			setup: func(c *Config) {
				c.Processes = append(c.Processes,
					ProcessConfig{Name: "test", Command: "echo 1", RestartPolicy: RestartAlways},
					ProcessConfig{Name: "test", Command: "echo 2", RestartPolicy: RestartAlways},
				)
			},
			wantErr: "duplicate process name: test",
		},
		{
			name: "invalid restart policy",
			setup: func(c *Config) {
				c.Processes = append(c.Processes, ProcessConfig{
					Name:          "test",
					Command:       "echo test",
					RestartPolicy: "invalid",
				})
			},
			wantErr: "invalid restart policy for process 'test'",
		},
		{
			name: "undefined dependency",
			setup: func(c *Config) {
				c.Processes = append(c.Processes, ProcessConfig{
					Name:          "test",
					Command:       "echo test",
					RestartPolicy: RestartAlways,
					DependsOn:     []string{"nonexistent"},
				})
			},
			wantErr: "process 'test' depends on undefined process 'nonexistent'",
		},
		{
			name: "circular dependency",
			setup: func(c *Config) {
				c.Processes = append(c.Processes,
					ProcessConfig{Name: "a", Command: "echo a", RestartPolicy: RestartAlways, DependsOn: []string{"b"}},
					ProcessConfig{Name: "b", Command: "echo b", RestartPolicy: RestartAlways, DependsOn: []string{"a"}},
				)
			},
			wantErr: "circular dependency detected",
		},
		{
			name: "valid configuration",
			setup: func(c *Config) {
				c.Processes = append(c.Processes,
					ProcessConfig{Name: "db", Command: "redis-server", RestartPolicy: RestartAlways},
					ProcessConfig{Name: "web", Command: "nginx", RestartPolicy: RestartAlways, DependsOn: []string{"db"}},
				)
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewConfig()
			tt.setup(config)
			
			err := config.Validate()
			
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestParseRestartPolicy(t *testing.T) {
	tests := []struct {
		input   string
		want    RestartPolicy
		wantErr bool
	}{
		{"always", RestartAlways, false},
		{"on-failure", RestartOnFailure, false},
		{"never", RestartNever, false},
		{"ALWAYS", RestartAlways, false},
		{"On-Failure", RestartOnFailure, false},
		{" never ", RestartNever, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseRestartPolicy(tt.input)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for input '%s', got nil", tt.input)
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				return
			}
			
			if got != tt.want {
				t.Errorf("For input '%s', expected %s, got %s", tt.input, tt.want, got)
			}
		})
	}
}

func TestParseLogFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    LogFormat
		wantErr bool
	}{
		{"json", LogFormatJSON, false},
		{"text", LogFormatText, false},
		{"JSON", LogFormatJSON, false},
		{"TEXT", LogFormatText, false},
		{" json ", LogFormatJSON, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLogFormat(tt.input)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for input '%s', got nil", tt.input)
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				return
			}
			
			if got != tt.want {
				t.Errorf("For input '%s', expected %s, got %s", tt.input, tt.want, got)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    LogLevel
		wantErr bool
	}{
		{"debug", LogLevelDebug, false},
		{"info", LogLevelInfo, false},
		{"warn", LogLevelWarn, false},
		{"error", LogLevelError, false},
		{"DEBUG", LogLevelDebug, false},
		{"INFO", LogLevelInfo, false},
		{" warn ", LogLevelWarn, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLogLevel(tt.input)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for input '%s', got nil", tt.input)
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				return
			}
			
			if got != tt.want {
				t.Errorf("For input '%s', expected %s, got %s", tt.input, tt.want, got)
			}
		})
	}
}

func TestValidateRestartPolicy(t *testing.T) {
	validPolicies := []RestartPolicy{RestartAlways, RestartOnFailure, RestartNever}
	for _, policy := range validPolicies {
		if err := validateRestartPolicy(policy); err != nil {
			t.Errorf("Expected valid policy %s to pass validation, got error: %v", policy, err)
		}
	}
	
	if err := validateRestartPolicy("invalid"); err == nil {
		t.Error("Expected invalid policy to fail validation")
	}
}

func TestValidateLogFormat(t *testing.T) {
	validFormats := []LogFormat{LogFormatJSON, LogFormatText}
	for _, format := range validFormats {
		if err := validateLogFormat(format); err != nil {
			t.Errorf("Expected valid format %s to pass validation, got error: %v", format, err)
		}
	}
	
	if err := validateLogFormat("invalid"); err == nil {
		t.Error("Expected invalid format to fail validation")
	}
}

func TestValidateLogLevel(t *testing.T) {
	validLevels := []LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}
	for _, level := range validLevels {
		if err := validateLogLevel(level); err != nil {
			t.Errorf("Expected valid level %s to pass validation, got error: %v", level, err)
		}
	}
	
	if err := validateLogLevel("invalid"); err == nil {
		t.Error("Expected invalid level to fail validation")
	}
}