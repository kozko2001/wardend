package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "always"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartNever     RestartPolicy = "never"
)

type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type ProcessConfig struct {
	Name           string
	Command        string
	RestartPolicy  RestartPolicy
	DependsOn      []string
	HealthCheck    string
	HealthInterval time.Duration
	StartRetries   int
	StartupTime    time.Duration
	MaxRestarts    int // -1 for infinite
	RestartDelay   time.Duration
}

type Config struct {
	Processes       []ProcessConfig
	LogFormat       LogFormat
	LogLevel        LogLevel
	LogDir          string
	ShutdownTimeout time.Duration
	RestartDelay    time.Duration
	StartRetries    int
	StartupTime     time.Duration
	MaxRestarts     int // -1 for infinite
	HealthInterval  time.Duration
	HTTPPort        int // 0 disables HTTP server
}

func NewConfig() *Config {
	return &Config{
		Processes:       make([]ProcessConfig, 0),
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelInfo,
		ShutdownTimeout: 10 * time.Second,
		RestartDelay:    1 * time.Second,
		StartRetries:    3,
		StartupTime:     60 * time.Second,
		MaxRestarts:     -1, // infinite by default
		HealthInterval:  30 * time.Second,
		HTTPPort:        0, // disabled by default
	}
}

func (c *Config) AddProcess(name, command string) *ProcessConfig {
	process := ProcessConfig{
		Name:           name,
		Command:        command,
		RestartPolicy:  RestartAlways,
		DependsOn:      make([]string, 0),
		HealthInterval: c.HealthInterval,
		StartRetries:   c.StartRetries,
		StartupTime:    c.StartupTime,
		MaxRestarts:    c.MaxRestarts,
		RestartDelay:   c.RestartDelay,
	}
	c.Processes = append(c.Processes, process)
	return &c.Processes[len(c.Processes)-1]
}

func (c *Config) Validate() error {
	if len(c.Processes) == 0 {
		return errors.New("no processes configured")
	}

	processNames := make(map[string]bool)
	for _, process := range c.Processes {
		if process.Name == "" {
			return errors.New("process name cannot be empty")
		}
		if process.Command == "" {
			return fmt.Errorf("command cannot be empty for process '%s'", process.Name)
		}
		if processNames[process.Name] {
			return fmt.Errorf("duplicate process name: %s", process.Name)
		}
		processNames[process.Name] = true

		if err := validateRestartPolicy(process.RestartPolicy); err != nil {
			return fmt.Errorf("invalid restart policy for process '%s': %v", process.Name, err)
		}

		for _, dep := range process.DependsOn {
			if !processNames[dep] && !hasDependency(c.Processes, dep) {
				return fmt.Errorf("process '%s' depends on undefined process '%s'", process.Name, dep)
			}
		}
	}

	if err := validateLogFormat(c.LogFormat); err != nil {
		return err
	}

	if err := validateLogLevel(c.LogLevel); err != nil {
		return err
	}

	if err := c.validateDependencies(); err != nil {
		return err
	}

	return nil
}

func hasDependency(processes []ProcessConfig, name string) bool {
	for _, p := range processes {
		if p.Name == name {
			return true
		}
	}
	return false
}

func validateRestartPolicy(policy RestartPolicy) error {
	switch policy {
	case RestartAlways, RestartOnFailure, RestartNever:
		return nil
	default:
		return fmt.Errorf("invalid restart policy: %s (must be one of: always, on-failure, never)", policy)
	}
}

func validateLogFormat(format LogFormat) error {
	switch format {
	case LogFormatJSON, LogFormatText:
		return nil
	default:
		return fmt.Errorf("invalid log format: %s (must be one of: json, text)", format)
	}
}

func validateLogLevel(level LogLevel) error {
	switch level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return nil
	default:
		return fmt.Errorf("invalid log level: %s (must be one of: debug, info, warn, error)", level)
	}
}

func (c *Config) validateDependencies() error {
	processMap := make(map[string]int)
	for i, process := range c.Processes {
		processMap[process.Name] = i
	}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(string) bool
	hasCycle = func(processName string) bool {
		visited[processName] = true
		recStack[processName] = true

		processIdx, exists := processMap[processName]
		if !exists {
			return false
		}

		for _, dep := range c.Processes[processIdx].DependsOn {
			if !visited[dep] {
				if hasCycle(dep) {
					return true
				}
			} else if recStack[dep] {
				return true
			}
		}

		recStack[processName] = false
		return false
	}

	for processName := range processMap {
		if !visited[processName] {
			if hasCycle(processName) {
				return errors.New("circular dependency detected in process dependencies")
			}
		}
	}

	return nil
}

func ParseRestartPolicy(s string) (RestartPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "always":
		return RestartAlways, nil
	case "on-failure":
		return RestartOnFailure, nil
	case "never":
		return RestartNever, nil
	default:
		return "", fmt.Errorf("invalid restart policy: %s", s)
	}
}

func ParseLogFormat(s string) (LogFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return LogFormatJSON, nil
	case "text":
		return LogFormatText, nil
	default:
		return "", fmt.Errorf("invalid log format: %s", s)
	}
}

func ParseLogLevel(s string) (LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LogLevelDebug, nil
	case "info":
		return LogLevelInfo, nil
	case "warn":
		return LogLevelWarn, nil
	case "error":
		return LogLevelError, nil
	default:
		return "", fmt.Errorf("invalid log level: %s", s)
	}
}

func ParseMaxRestarts(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "infinite" || s == "unlimited" {
		return -1, nil
	}

	value, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid max restarts value: %s (must be 'infinite' or positive integer)", s)
	}

	if value < 0 {
		return 0, fmt.Errorf("max restarts must be positive or 'infinite', got: %d", value)
	}

	return value, nil
}
