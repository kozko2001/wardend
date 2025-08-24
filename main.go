package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type arrayFlags []string

func (af *arrayFlags) String() string {
	return strings.Join(*af, ", ")
}

func (af *arrayFlags) Set(value string) error {
	*af = append(*af, value)
	return nil
}

type CLIArgs struct {
	runs              arrayFlags
	names             arrayFlags
	restartPolicies   arrayFlags
	dependsOn         arrayFlags
	healthChecks      arrayFlags
	healthIntervals   arrayFlags
	logFormat         string
	logLevel          string
	logDir            string
	shutdownTimeout   string
	restartDelay      string
	startRetries      string
	startSeconds      string
	maxRestarts       string
	defaultHealthInterval string
	help              bool
	version           bool
}

func main() {
	args := &CLIArgs{}
	
	flag.Var(&args.runs, "run", "Process command to run (can be specified multiple times)")
	flag.Var(&args.names, "name", "Name for the last --run process")
	flag.Var(&args.restartPolicies, "restart", "Restart policy: always|on-failure|never (default: always)")
	flag.Var(&args.dependsOn, "depends-on", "Process dependency (wait for named process)")
	flag.Var(&args.healthChecks, "health-check", "Health check command for process")
	flag.Var(&args.healthIntervals, "health-interval", "Health check interval (default: 30s)")
	
	flag.StringVar(&args.logFormat, "log-format", "text", "Log format: json|text (default: text)")
	flag.StringVar(&args.logLevel, "log-level", "info", "Log level: debug|info|warn|error (default: info)")
	flag.StringVar(&args.logDir, "log-dir", "", "Directory for per-process logs")
	flag.StringVar(&args.shutdownTimeout, "shutdown-timeout", "10s", "Graceful shutdown timeout (default: 10s)")
	flag.StringVar(&args.restartDelay, "restart-delay", "1s", "Delay between restart attempts (default: 1s)")
	flag.StringVar(&args.startRetries, "start-retries", "3", "Max startup failure attempts before giving up (default: 3)")
	flag.StringVar(&args.startSeconds, "start-seconds", "60s", "Time process must run to be considered successfully started (default: 60s)")
	flag.StringVar(&args.maxRestarts, "max-restarts", "infinite", "Max runtime restart attempts (default: infinite)")
	flag.StringVar(&args.defaultHealthInterval, "health-interval-default", "30s", "Default health check interval (default: 30s)")
	flag.BoolVar(&args.help, "help", false, "Show help")
	flag.BoolVar(&args.help, "h", false, "Show help")
	flag.BoolVar(&args.version, "version", false, "Show version")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Wardend - The SQLite of process management for Docker containers

Usage: %s [OPTIONS]

A single-binary, configuration-optional process supervisor designed specifically 
for containerized environments. Run multiple processes with proper signal handling,
restart policies, health checks, and dependency management.

Examples:
  # Simple multi-process container
  %s --run "nginx -g 'daemon off;'" --name web --restart always \
%s --run "python worker.py" --name worker --restart on-failure

  # Advanced configuration with dependencies
  %s --run "redis-server --daemonize no" --name cache --restart always \
%s --run "nginx -g 'daemon off;'" --name web --depends-on cache \
%s --run "gunicorn app:app" --name app --depends-on cache \
%s --log-format json --shutdown-timeout 30s

OPTIONS:
`, os.Args[0], os.Args[0], strings.Repeat(" ", len(os.Args[0])), os.Args[0], strings.Repeat(" ", len(os.Args[0])), strings.Repeat(" ", len(os.Args[0])), strings.Repeat(" ", len(os.Args[0])))
		flag.PrintDefaults()
	}
	
	flag.Parse()

	if args.help {
		flag.Usage()
		os.Exit(0)
	}

	if args.version {
		fmt.Println("wardend version 0.1.0")
		os.Exit(0)
	}

	config, err := buildConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}


	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	manager := NewManager(config)

	if err := manager.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize manager: %v\n", err)
		os.Exit(1)
	}

	manager.HandleSignals()

	fmt.Printf("Wardend starting with %d processes\n", len(config.Processes))
	for _, process := range config.Processes {
		fmt.Printf("  - %s: %s (restart: %s)\n", process.Name, process.Command, process.RestartPolicy)
		if len(process.DependsOn) > 0 {
			fmt.Printf("    depends on: %s\n", strings.Join(process.DependsOn, ", "))
		}
		if process.HealthCheck != "" {
			fmt.Printf("    health check: %s (every %s)\n", process.HealthCheck, process.HealthInterval)
		}
	}

	if err := manager.StartAll(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start processes: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All processes started successfully")
	manager.Wait()
}

func buildConfig(args *CLIArgs) (*Config, error) {
	config := NewConfig()

	if len(args.runs) == 0 {
		return nil, fmt.Errorf("at least one --run command is required")
	}

	logFormat, err := ParseLogFormat(args.logFormat)
	if err != nil {
		return nil, err
	}
	config.LogFormat = logFormat

	logLevel, err := ParseLogLevel(args.logLevel)
	if err != nil {
		return nil, err
	}
	config.LogLevel = logLevel

	config.LogDir = args.logDir

	shutdownTimeout, err := time.ParseDuration(args.shutdownTimeout)
	if err != nil {
		return nil, fmt.Errorf("invalid shutdown timeout: %v", err)
	}
	config.ShutdownTimeout = shutdownTimeout

	restartDelay, err := time.ParseDuration(args.restartDelay)
	if err != nil {
		return nil, fmt.Errorf("invalid restart delay: %v", err)
	}
	config.RestartDelay = restartDelay

	startRetries, err := strconv.Atoi(args.startRetries)
	if err != nil {
		return nil, fmt.Errorf("invalid start retries: %v", err)
	}
	if startRetries < 0 {
		return nil, fmt.Errorf("start retries must be non-negative, got: %d", startRetries)
	}
	config.StartRetries = startRetries

	startupTime, err := time.ParseDuration(args.startSeconds)
	if err != nil {
		return nil, fmt.Errorf("invalid startup time: %v", err)
	}
	config.StartupTime = startupTime

	maxRestarts, err := ParseMaxRestarts(args.maxRestarts)
	if err != nil {
		return nil, err
	}
	config.MaxRestarts = maxRestarts

	defaultHealthInterval, err := time.ParseDuration(args.defaultHealthInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid default health interval: %v", err)
	}
	config.HealthInterval = defaultHealthInterval

	nameIndex := 0
	restartIndex := 0
	dependsIndex := 0
	healthCheckIndex := 0
	healthIntervalIndex := 0

	for i, command := range args.runs {
		var name string
		if nameIndex < len(args.names) {
			name = args.names[nameIndex]
			nameIndex++
		} else {
			name = fmt.Sprintf("process-%d", i+1)
		}

		process := config.AddProcess(name, command)

		if restartIndex < len(args.restartPolicies) {
			policy, err := ParseRestartPolicy(args.restartPolicies[restartIndex])
			if err != nil {
				return nil, fmt.Errorf("invalid restart policy for process %s: %v", name, err)
			}
			process.RestartPolicy = policy
			restartIndex++
		}

		if dependsIndex < len(args.dependsOn) {
			deps := strings.Split(args.dependsOn[dependsIndex], ",")
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					process.DependsOn = append(process.DependsOn, dep)
				}
			}
			dependsIndex++
		}

		if healthCheckIndex < len(args.healthChecks) {
			process.HealthCheck = args.healthChecks[healthCheckIndex]
			healthCheckIndex++
		}

		if healthIntervalIndex < len(args.healthIntervals) {
			intervalStr := strings.TrimSpace(args.healthIntervals[healthIntervalIndex])
			if intervalStr != "" {
				interval, err := time.ParseDuration(intervalStr)
				if err != nil {
					return nil, fmt.Errorf("invalid health interval for process %s: %v", name, err)
				}
				process.HealthInterval = interval
			}
			healthIntervalIndex++
		}
	}

	return config, nil
}