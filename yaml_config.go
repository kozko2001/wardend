package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the YAML configuration structure
type YAMLConfig struct {
	Processes       []YAMLProcessConfig `yaml:"processes"`
	LogFormat       string              `yaml:"log_format,omitempty"`
	LogLevel        string              `yaml:"log_level,omitempty"`
	LogDir          string              `yaml:"log_dir,omitempty"`
	ShutdownTimeout string              `yaml:"shutdown_timeout,omitempty"`
	RestartDelay    string              `yaml:"restart_delay,omitempty"`
	StartRetries    int                 `yaml:"start_retries,omitempty"`
	StartupTime     string              `yaml:"startup_time,omitempty"`
	MaxRestarts     string              `yaml:"max_restarts,omitempty"`
	HealthInterval  string              `yaml:"health_interval,omitempty"`
	HTTPPort        int                 `yaml:"monitor_http_port,omitempty"`
}

// YAMLProcessConfig represents a process configuration in YAML
type YAMLProcessConfig struct {
	Name           string   `yaml:"name"`
	Command        string   `yaml:"command"`
	RestartPolicy  string   `yaml:"restart_policy,omitempty"`
	DependsOn      []string `yaml:"depends_on,omitempty"`
	HealthCheck    string   `yaml:"health_check,omitempty"`
	HealthInterval string   `yaml:"health_interval,omitempty"`
	StartRetries   int      `yaml:"start_retries,omitempty"`
	StartupTime    string   `yaml:"startup_time,omitempty"`
	MaxRestarts    string   `yaml:"max_restarts,omitempty"`
	RestartDelay   string   `yaml:"restart_delay,omitempty"`
}

// LoadYAMLConfig loads configuration from a YAML file
func LoadYAMLConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var yamlConfig YAMLConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %v", err)
	}

	return yamlConfig.ToConfig()
}

// ToConfig converts YAMLConfig to Config
func (yc *YAMLConfig) ToConfig() (*Config, error) {
	config := NewConfig()

	// Parse global settings
	if yc.LogFormat != "" {
		logFormat, err := ParseLogFormat(yc.LogFormat)
		if err != nil {
			return nil, fmt.Errorf("invalid log_format: %v", err)
		}
		config.LogFormat = logFormat
	}

	if yc.LogLevel != "" {
		logLevel, err := ParseLogLevel(yc.LogLevel)
		if err != nil {
			return nil, fmt.Errorf("invalid log_level: %v", err)
		}
		config.LogLevel = logLevel
	}

	if yc.LogDir != "" {
		config.LogDir = yc.LogDir
	}

	if yc.ShutdownTimeout != "" {
		duration, err := time.ParseDuration(yc.ShutdownTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid shutdown_timeout: %v", err)
		}
		config.ShutdownTimeout = duration
	}

	if yc.RestartDelay != "" {
		duration, err := time.ParseDuration(yc.RestartDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid restart_delay: %v", err)
		}
		config.RestartDelay = duration
	}

	if yc.StartRetries > 0 {
		config.StartRetries = yc.StartRetries
	}

	if yc.StartupTime != "" {
		duration, err := time.ParseDuration(yc.StartupTime)
		if err != nil {
			return nil, fmt.Errorf("invalid startup_time: %v", err)
		}
		config.StartupTime = duration
	}

	if yc.MaxRestarts != "" {
		maxRestarts, err := ParseMaxRestarts(yc.MaxRestarts)
		if err != nil {
			return nil, fmt.Errorf("invalid max_restarts: %v", err)
		}
		config.MaxRestarts = maxRestarts
	}

	if yc.HealthInterval != "" {
		duration, err := time.ParseDuration(yc.HealthInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid health_interval: %v", err)
		}
		config.HealthInterval = duration
	}

	if yc.HTTPPort > 0 {
		config.HTTPPort = yc.HTTPPort
	}

	// Parse processes
	for _, yamlProcess := range yc.Processes {
		processConfig, err := yamlProcess.ToProcessConfig(config)
		if err != nil {
			return nil, fmt.Errorf("invalid process '%s': %v", yamlProcess.Name, err)
		}
		config.Processes = append(config.Processes, *processConfig)
	}

	return config, nil
}

// ToProcessConfig converts YAMLProcessConfig to ProcessConfig
func (yp *YAMLProcessConfig) ToProcessConfig(globalConfig *Config) (*ProcessConfig, error) {
	if yp.Name == "" {
		return nil, fmt.Errorf("process name cannot be empty")
	}

	if yp.Command == "" {
		return nil, fmt.Errorf("process command cannot be empty")
	}

	process := ProcessConfig{
		Name:           yp.Name,
		Command:        yp.Command,
		RestartPolicy:  RestartAlways,
		DependsOn:      yp.DependsOn,
		HealthCheck:    yp.HealthCheck,
		HealthInterval: globalConfig.HealthInterval,
		StartRetries:   globalConfig.StartRetries,
		StartupTime:    globalConfig.StartupTime,
		MaxRestarts:    globalConfig.MaxRestarts,
		RestartDelay:   globalConfig.RestartDelay,
	}

	// Parse restart policy
	if yp.RestartPolicy != "" {
		restartPolicy, err := ParseRestartPolicy(yp.RestartPolicy)
		if err != nil {
			return nil, fmt.Errorf("invalid restart_policy: %v", err)
		}
		process.RestartPolicy = restartPolicy
	}

	// Parse health interval
	if yp.HealthInterval != "" {
		duration, err := time.ParseDuration(yp.HealthInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid health_interval: %v", err)
		}
		process.HealthInterval = duration
	}

	// Parse start retries
	if yp.StartRetries > 0 {
		process.StartRetries = yp.StartRetries
	}

	// Parse startup time
	if yp.StartupTime != "" {
		duration, err := time.ParseDuration(yp.StartupTime)
		if err != nil {
			return nil, fmt.Errorf("invalid startup_time: %v", err)
		}
		process.StartupTime = duration
	}

	// Parse max restarts
	if yp.MaxRestarts != "" {
		maxRestarts, err := ParseMaxRestarts(yp.MaxRestarts)
		if err != nil {
			return nil, fmt.Errorf("invalid max_restarts: %v", err)
		}
		process.MaxRestarts = maxRestarts
	}

	// Parse restart delay
	if yp.RestartDelay != "" {
		duration, err := time.ParseDuration(yp.RestartDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid restart_delay: %v", err)
		}
		process.RestartDelay = duration
	}

	return &process, nil
}

// SaveYAMLConfig saves configuration to a YAML file
func SaveYAMLConfig(config *Config, filename string) error {
	yamlConfig := FromConfig(config)

	data, err := yaml.Marshal(yamlConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %v", err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// FromConfig converts Config to YAMLConfig
func FromConfig(config *Config) *YAMLConfig {
	yamlConfig := &YAMLConfig{
		LogFormat:       string(config.LogFormat),
		LogLevel:        string(config.LogLevel),
		LogDir:          config.LogDir,
		ShutdownTimeout: config.ShutdownTimeout.String(),
		RestartDelay:    config.RestartDelay.String(),
		StartRetries:    config.StartRetries,
		StartupTime:     config.StartupTime.String(),
		HealthInterval:  config.HealthInterval.String(),
		HTTPPort:        config.HTTPPort,
		Processes:       make([]YAMLProcessConfig, 0, len(config.Processes)),
	}

	if config.MaxRestarts == -1 {
		yamlConfig.MaxRestarts = "infinite"
	} else {
		yamlConfig.MaxRestarts = fmt.Sprintf("%d", config.MaxRestarts)
	}

	for _, process := range config.Processes {
		yamlProcess := YAMLProcessConfig{
			Name:           process.Name,
			Command:        process.Command,
			RestartPolicy:  string(process.RestartPolicy),
			DependsOn:      process.DependsOn,
			HealthCheck:    process.HealthCheck,
			HealthInterval: process.HealthInterval.String(),
			StartRetries:   process.StartRetries,
			StartupTime:    process.StartupTime.String(),
			RestartDelay:   process.RestartDelay.String(),
		}

		if process.MaxRestarts == -1 {
			yamlProcess.MaxRestarts = "infinite"
		} else {
			yamlProcess.MaxRestarts = fmt.Sprintf("%d", process.MaxRestarts)
		}

		yamlConfig.Processes = append(yamlConfig.Processes, yamlProcess)
	}

	return yamlConfig
}
