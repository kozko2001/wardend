package main

import (
	"strings"
	"testing"
	"time"
)

func TestArrayFlags(t *testing.T) {
	var af arrayFlags

	if af.String() != "" {
		t.Errorf("Expected empty string for uninitialized arrayFlags, got '%s'", af.String())
	}

	_ = af.Set("first")
	_ = af.Set("second")

	result := af.String()
	if result != "first, second" {
		t.Errorf("Expected 'first, second', got '%s'", result)
	}
}

func TestBuildConfigBasic(t *testing.T) {
	args := &CLIArgs{
		runs:                  arrayFlags{"echo hello"},
		names:                 arrayFlags{"test"},
		logFormat:             "json",
		logLevel:              "debug",
		shutdownTimeout:       "15s",
		restartDelay:          "2s",
		startRetries:          "2",
		startSeconds:          "30s",
		maxRestarts:           "10",
		defaultHealthInterval: "60s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(config.Processes) != 1 {
		t.Errorf("Expected 1 process, got %d", len(config.Processes))
	}

	process := config.Processes[0]
	if process.Name != "test" {
		t.Errorf("Expected process name 'test', got '%s'", process.Name)
	}

	if process.Command != "echo hello" {
		t.Errorf("Expected command 'echo hello', got '%s'", process.Command)
	}

	if config.LogFormat != LogFormatJSON {
		t.Errorf("Expected JSON log format, got %s", config.LogFormat)
	}

	if config.LogLevel != LogLevelDebug {
		t.Errorf("Expected debug log level, got %s", config.LogLevel)
	}

	if config.ShutdownTimeout != 15*time.Second {
		t.Errorf("Expected 15s shutdown timeout, got %s", config.ShutdownTimeout)
	}

	if config.RestartDelay != 2*time.Second {
		t.Errorf("Expected 2s restart delay, got %s", config.RestartDelay)
	}

	if config.MaxRestarts != 10 {
		t.Errorf("Expected 10 max restarts, got %d", config.MaxRestarts)
	}

	if config.HealthInterval != 60*time.Second {
		t.Errorf("Expected 60s health interval, got %s", config.HealthInterval)
	}
}

func TestBuildConfigMultipleProcesses(t *testing.T) {
	args := &CLIArgs{
		runs:                  arrayFlags{"redis-server", "nginx", "python app.py"},
		names:                 arrayFlags{"redis", "web", "app"},
		restartPolicies:       arrayFlags{"always", "always", "on-failure"},
		dependsOn:             arrayFlags{"", "redis", "redis,web"},
		healthChecks:          arrayFlags{"redis-cli ping", "curl localhost", ""},
		healthIntervals:       arrayFlags{"10s", "30s", ""},
		logFormat:             "text",
		logLevel:              "info",
		shutdownTimeout:       "10s",
		restartDelay:          "1s",
		startRetries:          "3",
		startSeconds:          "60s",
		maxRestarts:           "5",
		defaultHealthInterval: "30s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(config.Processes) != 3 {
		t.Errorf("Expected 3 processes, got %d", len(config.Processes))
	}

	redis := config.Processes[0]
	if redis.Name != "redis" || redis.Command != "redis-server" {
		t.Errorf("Redis process not configured correctly: name=%s, cmd=%s", redis.Name, redis.Command)
	}
	if redis.RestartPolicy != RestartAlways {
		t.Errorf("Expected redis restart policy 'always', got %s", redis.RestartPolicy)
	}
	if len(redis.DependsOn) != 0 {
		t.Errorf("Expected redis to have no dependencies, got %d", len(redis.DependsOn))
	}
	if redis.HealthCheck != "redis-cli ping" {
		t.Errorf("Expected redis health check 'redis-cli ping', got '%s'", redis.HealthCheck)
	}
	if redis.HealthInterval != 10*time.Second {
		t.Errorf("Expected redis health interval 10s, got %s", redis.HealthInterval)
	}

	web := config.Processes[1]
	if web.Name != "web" || web.Command != "nginx" {
		t.Errorf("Web process not configured correctly: name=%s, cmd=%s", web.Name, web.Command)
	}
	if len(web.DependsOn) != 1 || web.DependsOn[0] != "redis" {
		t.Errorf("Expected web to depend on redis, got %v", web.DependsOn)
	}

	app := config.Processes[2]
	if app.Name != "app" || app.Command != "python app.py" {
		t.Errorf("App process not configured correctly: name=%s, cmd=%s", app.Name, app.Command)
	}
	if app.RestartPolicy != RestartOnFailure {
		t.Errorf("Expected app restart policy 'on-failure', got %s", app.RestartPolicy)
	}
	if len(app.DependsOn) != 2 {
		t.Errorf("Expected app to have 2 dependencies, got %d", len(app.DependsOn))
	}
	expectedDeps := []string{"redis", "web"}
	for i, dep := range expectedDeps {
		if i >= len(app.DependsOn) || app.DependsOn[i] != dep {
			t.Errorf("Expected app dependency %d to be '%s', got '%s'", i, dep, app.DependsOn[i])
		}
	}
}

func TestBuildConfigDefaultNames(t *testing.T) {
	args := &CLIArgs{
		runs:                  arrayFlags{"echo 1", "echo 2", "echo 3"},
		logFormat:             "text",
		logLevel:              "info",
		shutdownTimeout:       "10s",
		restartDelay:          "1s",
		startRetries:          "3",
		startSeconds:          "60s",
		maxRestarts:           "5",
		defaultHealthInterval: "30s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedNames := []string{"process-1", "process-2", "process-3"}
	for i, expectedName := range expectedNames {
		if config.Processes[i].Name != expectedName {
			t.Errorf("Expected process %d name '%s', got '%s'", i, expectedName, config.Processes[i].Name)
		}
	}
}

func TestBuildConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    *CLIArgs
		wantErr string
	}{
		{
			name: "no run commands",
			args: &CLIArgs{
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "at least one --run command or cron job (--cron-schedule + --cron-command) is required",
		},
		{
			name: "invalid log format",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "invalid",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid log format",
		},
		{
			name: "invalid log level",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "text",
				logLevel:              "invalid",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid log level",
		},
		{
			name: "invalid shutdown timeout",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "invalid",
				restartDelay:          "1s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid shutdown timeout",
		},
		{
			name: "invalid restart delay",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "invalid",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid restart delay",
		},
		{
			name: "invalid max restarts",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				startRetries:          "3",
				startSeconds:          "60s",
				maxRestarts:           "invalid",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid max restarts",
		},
		{
			name: "invalid default health interval",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				startRetries:          "3",
				startSeconds:          "60s",
				maxRestarts:           "5",
				defaultHealthInterval: "invalid",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid default health interval",
		},
		{
			name: "invalid restart policy",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				restartPolicies:       arrayFlags{"invalid"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				startRetries:          "3",
				startSeconds:          "60s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid restart policy",
		},
		{
			name: "invalid health interval",
			args: &CLIArgs{
				runs:                  arrayFlags{"echo hello"},
				healthIntervals:       arrayFlags{"invalid"},
				logFormat:             "text",
				logLevel:              "info",
				shutdownTimeout:       "10s",
				restartDelay:          "1s",
				startRetries:          "3",
				startSeconds:          "60s",
				maxRestarts:           "5",
				defaultHealthInterval: "30s",
				monitorHTTPPort:       "0",
			},
			wantErr: "invalid health interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildConfig(tt.args)

			if err == nil {
				t.Errorf("Expected error containing '%s', got nil", tt.wantErr)
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing '%s', got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestBuildConfigDependencyParsing(t *testing.T) {
	args := &CLIArgs{
		runs:                  arrayFlags{"echo app"},
		names:                 arrayFlags{"app"},
		dependsOn:             arrayFlags{"redis,nginx, worker"},
		logFormat:             "text",
		logLevel:              "info",
		shutdownTimeout:       "10s",
		restartDelay:          "1s",
		startRetries:          "3",
		startSeconds:          "60s",
		maxRestarts:           "5",
		defaultHealthInterval: "30s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedDeps := []string{"redis", "nginx", "worker"}
	process := config.Processes[0]

	if len(process.DependsOn) != len(expectedDeps) {
		t.Errorf("Expected %d dependencies, got %d", len(expectedDeps), len(process.DependsOn))
	}

	for i, expectedDep := range expectedDeps {
		if i >= len(process.DependsOn) || process.DependsOn[i] != expectedDep {
			t.Errorf("Expected dependency %d to be '%s', got '%s'", i, expectedDep, process.DependsOn[i])
		}
	}
}

func TestBuildConfigMixedDependencies(t *testing.T) {
	// This test demonstrates the CLI parsing behavior where empty strings
	// are needed for processes without dependencies when other processes have them
	args := &CLIArgs{
		runs:                  arrayFlags{"echo mysql", "echo redis", "echo php", "echo nginx"},
		names:                 arrayFlags{"mysql", "redis", "php", "nginx"},
		dependsOn:             arrayFlags{"", "mysql", "mysql,redis", "php"},
		logFormat:             "text",
		logLevel:              "info",
		shutdownTimeout:       "10s",
		restartDelay:          "1s",
		startRetries:          "3",
		startSeconds:          "60s",
		maxRestarts:           "infinite",
		defaultHealthInterval: "30s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(config.Processes) != 4 {
		t.Errorf("Expected 4 processes, got %d", len(config.Processes))
	}

	// mysql: no dependencies (empty string)
	mysql := config.Processes[0]
	if len(mysql.DependsOn) != 0 {
		t.Errorf("Expected mysql to have no dependencies, got %v", mysql.DependsOn)
	}

	// redis: depends on mysql
	redis := config.Processes[1]
	if len(redis.DependsOn) != 1 || redis.DependsOn[0] != "mysql" {
		t.Errorf("Expected redis to depend on mysql, got %v", redis.DependsOn)
	}

	// php: depends on mysql and redis
	php := config.Processes[2]
	if len(php.DependsOn) != 2 {
		t.Errorf("Expected php to have 2 dependencies, got %d", len(php.DependsOn))
	}
	expectedPhpDeps := []string{"mysql", "redis"}
	for i, expected := range expectedPhpDeps {
		if i >= len(php.DependsOn) || php.DependsOn[i] != expected {
			t.Errorf("Expected php dependency %d to be '%s', got '%s'", i, expected, php.DependsOn[i])
		}
	}

	// nginx: depends on php
	nginx := config.Processes[3]
	if len(nginx.DependsOn) != 1 || nginx.DependsOn[0] != "php" {
		t.Errorf("Expected nginx to depend on php, got %v", nginx.DependsOn)
	}

	// Verify the dependency chain creates correct order
	err = config.validateDependencies()
	if err != nil {
		t.Errorf("Dependency validation should pass, got error: %v", err)
	}
}

func TestBuildConfigSparseDependencies(t *testing.T) {
	// This test verifies that we can specify dependencies for only some processes
	// without requiring empty strings for processes that have no dependencies
	args := &CLIArgs{
		runs:                  arrayFlags{"echo mysql", "echo redis", "echo php", "echo nginx"},
		names:                 arrayFlags{"mysql", "redis", "php", "nginx"},
		dependsOn:             arrayFlags{"mysql", "mysql,redis", "php"}, // Only 3 deps for 4 processes
		logFormat:             "text",
		logLevel:              "info",
		shutdownTimeout:       "10s",
		restartDelay:          "1s",
		startRetries:          "3",
		startSeconds:          "60s",
		maxRestarts:           "infinite",
		defaultHealthInterval: "30s",
		monitorHTTPPort:       "0",
	}

	config, err := buildConfig(args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(config.Processes) != 4 {
		t.Errorf("Expected 4 processes, got %d", len(config.Processes))
	}

	// mysql: no dependencies (first process, before any --depends-on flags)
	mysql := config.Processes[0]
	if len(mysql.DependsOn) != 0 {
		t.Errorf("Expected mysql to have no dependencies, got %v", mysql.DependsOn)
	}

	// redis: depends on mysql (first --depends-on flag)
	redis := config.Processes[1] 
	if len(redis.DependsOn) != 1 || redis.DependsOn[0] != "mysql" {
		t.Errorf("Expected redis to depend on mysql, got %v", redis.DependsOn)
	}

	// php: depends on mysql and redis (second --depends-on flag)
	php := config.Processes[2]
	if len(php.DependsOn) != 2 {
		t.Errorf("Expected php to have 2 dependencies, got %d", len(php.DependsOn))
	}
	expectedPhpDeps := []string{"mysql", "redis"}
	for i, expected := range expectedPhpDeps {
		if i >= len(php.DependsOn) || php.DependsOn[i] != expected {
			t.Errorf("Expected php dependency %d to be '%s', got '%s'", i, expected, php.DependsOn[i])
		}
	}

	// nginx: depends on php (third --depends-on flag)
	nginx := config.Processes[3]
	if len(nginx.DependsOn) != 1 || nginx.DependsOn[0] != "php" {
		t.Errorf("Expected nginx to depend on php, got %v", nginx.DependsOn)
	}

	// Verify no circular dependencies
	err = config.validateDependencies()
	if err != nil {
		t.Errorf("Dependency validation should pass, got error: %v", err)
	}
}
