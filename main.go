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
	runs                  arrayFlags
	names                 arrayFlags
	restartPolicies       arrayFlags
	dependsOn             arrayFlags
	healthChecks          arrayFlags
	healthIntervals       arrayFlags
	cronSchedules         arrayFlags
	cronCommands          arrayFlags
	cronNames             arrayFlags
	cronRetries           arrayFlags
	cronTimeouts          arrayFlags
	config                string
	logFormat             string
	logLevel              string
	logDir                string
	shutdownTimeout       string
	restartDelay          string
	startRetries          string
	startSeconds          string
	maxRestarts           string
	defaultHealthInterval string
	monitorHTTPPort       string
	help                  bool
	version               bool
}

func main() {
	args := &CLIArgs{}

	flag.Var(&args.runs, "run", "Process command to run (can be specified multiple times)")
	flag.Var(&args.names, "name", "Name for the last --run process")
	flag.Var(&args.restartPolicies, "restart", "Restart policy: always|on-failure|never (default: always)")
	flag.Var(&args.dependsOn, "depends-on", "Process dependency (wait for named process)")
	flag.Var(&args.healthChecks, "health-check", "Health check command for process")
	flag.Var(&args.healthIntervals, "health-interval", "Health check interval (default: 30s)")
	
	flag.Var(&args.cronSchedules, "cron-schedule", "Cron job schedule (e.g., 'daily', 'every 5m', '0 2 * * *')")
	flag.Var(&args.cronCommands, "cron-command", "Command to execute for cron job")
	flag.Var(&args.cronNames, "cron-name", "Name for the cron job (optional, auto-generated if not provided)")
	flag.Var(&args.cronRetries, "cron-retries", "Max retry attempts for cron job (default: 3)")
	flag.Var(&args.cronTimeouts, "cron-timeout", "Execution timeout for cron job (default: 10m)")

	flag.StringVar(&args.config, "config", "", "Path to YAML configuration file")
	flag.StringVar(&args.logFormat, "log-format", "text", "Log format: json|text (default: text)")
	flag.StringVar(&args.logLevel, "log-level", "info", "Log level: debug|info|warn|error (default: info)")
	flag.StringVar(&args.logDir, "log-dir", "", "Directory for per-process logs")
	flag.StringVar(&args.shutdownTimeout, "shutdown-timeout", "10s", "Graceful shutdown timeout (default: 10s)")
	flag.StringVar(&args.restartDelay, "restart-delay", "1s", "Delay between restart attempts (default: 1s)")
	flag.StringVar(&args.startRetries, "start-retries", "3", "Max startup failure attempts before giving up (default: 3)")
	flag.StringVar(&args.startSeconds, "start-seconds", "60s", "Time process must run to be considered successfully started (default: 60s)")
	flag.StringVar(&args.maxRestarts, "max-restarts", "infinite", "Max runtime restart attempts (default: infinite)")
	flag.StringVar(&args.defaultHealthInterval, "health-interval-default", "30s", "Default health check interval (default: 30s)")
	flag.StringVar(&args.monitorHTTPPort, "monitor-http-port", "0", "HTTP monitoring/health check server port (0 disables, default: 0)")
	flag.BoolVar(&args.help, "help", false, "Show help")
	flag.BoolVar(&args.help, "h", false, "Show help")
	flag.BoolVar(&args.version, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Wardend - The SQLite of process management for Docker containers

Usage: %s [OPTIONS]

A single-binary, configuration-optional process supervisor designed specifically 
for containerized environments. Run multiple processes with proper signal handling,
restart policies, health checks, dependency management, and cron scheduling.

Examples:
  # Using YAML configuration file
  %s --config wardend.yml

  # Simple multi-process container
  %s --run "nginx -g 'daemon off;'" --name web --restart always \
%s --run "python worker.py" --name worker --restart on-failure

  # Advanced configuration with dependencies
  %s --run "redis-server --daemonize no" --name cache --restart always \
%s --run "nginx -g 'daemon off;'" --name web --depends-on cache \
%s --run "gunicorn app:app" --name app --depends-on cache \
%s --log-format json --shutdown-timeout 30s

  # With cron jobs (human-readable schedules)
  %s --run "nginx -g 'daemon off;'" --name web \
%s --cron-schedule "daily" --cron-command "backup-db.sh" --cron-name backup \
%s --cron-schedule "every 15m" --cron-command "health-check.sh" --cron-retries 2

  # Traditional cron expressions also supported
  %s --cron-schedule "0 2 * * *" --cron-command "backup.sh" --cron-timeout 30m

OPTIONS:
`, os.Args[0], os.Args[0], os.Args[0], strings.Repeat(" ", len(os.Args[0])), os.Args[0], strings.Repeat(" ", len(os.Args[0])), strings.Repeat(" ", len(os.Args[0])), strings.Repeat(" ", len(os.Args[0])), os.Args[0], strings.Repeat(" ", len(os.Args[0])), strings.Repeat(" ", len(os.Args[0])), os.Args[0])
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

	var config *Config
	var err error

	if args.config != "" {
		// Load from YAML file
		config, err = LoadYAMLConfig(args.config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config file: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Build from CLI arguments
		config, err = buildConfig(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	manager, err := NewManager(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := manager.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize manager: %v\n", err)
		os.Exit(1)
	}

	manager.HandleSignals()

	fmt.Printf("Wardend starting with %d processes", len(config.Processes))
	if len(config.CronJobs) > 0 {
		fmt.Printf(" and %d cron jobs", len(config.CronJobs))
	}
	fmt.Println()
	
	for _, process := range config.Processes {
		fmt.Printf("  - %s: %s (restart: %s)\n", process.Name, process.Command, process.RestartPolicy)
		if len(process.DependsOn) > 0 {
			fmt.Printf("    depends on: %s\n", strings.Join(process.DependsOn, ", "))
		}
		if process.HealthCheck != "" {
			fmt.Printf("    health check: %s (every %s)\n", process.HealthCheck, process.HealthInterval)
		}
	}
	
	for _, cronJob := range config.CronJobs {
		fmt.Printf("  - %s: %s (schedule: %s)\n", cronJob.Name, cronJob.Command, cronJob.Schedule)
		fmt.Printf("    retries: %d, timeout: %s\n", cronJob.Retries, cronJob.Timeout)
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

	if len(args.runs) == 0 && len(args.cronSchedules) == 0 {
		return nil, fmt.Errorf("at least one --run command or cron job (--cron-schedule + --cron-command) is required")
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

	monitorHTTPPort, err := strconv.Atoi(args.monitorHTTPPort)
	if err != nil {
		return nil, fmt.Errorf("invalid monitor HTTP port: %v", err)
	}
	if monitorHTTPPort < 0 || monitorHTTPPort > 65535 {
		return nil, fmt.Errorf("monitor HTTP port must be between 0 and 65535, got: %d", monitorHTTPPort)
	}
	config.HTTPPort = monitorHTTPPort

	for i, command := range args.runs {
		var name string
		if i < len(args.names) {
			name = args.names[i]
		} else {
			name = fmt.Sprintf("process-%d", i+1)
		}

		process := config.AddProcess(name, command)

		if i < len(args.restartPolicies) {
			policy, err := ParseRestartPolicy(args.restartPolicies[i])
			if err != nil {
				return nil, fmt.Errorf("invalid restart policy for process %s: %v", name, err)
			}
			process.RestartPolicy = policy
		}

		// Handle dependencies - when there are fewer --depends-on flags than processes,
		// we need to assign them correctly. The key insight is that a --depends-on flag
		// is meant for the process that comes after all the processes it depends on.
		depIdx := i - (len(args.runs) - len(args.dependsOn))
		if depIdx >= 0 && depIdx < len(args.dependsOn) {
			deps := strings.Split(args.dependsOn[depIdx], ",")
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					process.DependsOn = append(process.DependsOn, dep)
				}
			}
		}

		if i < len(args.healthChecks) {
			process.HealthCheck = args.healthChecks[i]
		}

		if i < len(args.healthIntervals) {
			intervalStr := strings.TrimSpace(args.healthIntervals[i])
			if intervalStr != "" {
				interval, err := time.ParseDuration(intervalStr)
				if err != nil {
					return nil, fmt.Errorf("invalid health interval for process %s: %v", name, err)
				}
				process.HealthInterval = interval
			}
		}
	}

	// Process cron jobs - validate that schedules and commands are paired
	if len(args.cronSchedules) != len(args.cronCommands) {
		return nil, fmt.Errorf("mismatch between --cron-schedule (%d) and --cron-command (%d) flags - they must be paired", 
			len(args.cronSchedules), len(args.cronCommands))
	}

	for i := 0; i < len(args.cronSchedules); i++ {
		schedule := strings.TrimSpace(args.cronSchedules[i])
		command := strings.TrimSpace(args.cronCommands[i])
		
		if schedule == "" {
			return nil, fmt.Errorf("cron schedule %d cannot be empty", i+1)
		}
		if command == "" {
			return nil, fmt.Errorf("cron command %d cannot be empty", i+1)
		}

		var name string
		if i < len(args.cronNames) && strings.TrimSpace(args.cronNames[i]) != "" {
			name = strings.TrimSpace(args.cronNames[i])
		} else {
			name = "" // Let AddCronJob auto-generate the name
		}

		cronJob := config.AddCronJob(name, schedule, command)

		// Apply cron-specific settings
		if i < len(args.cronRetries) {
			retriesStr := strings.TrimSpace(args.cronRetries[i])
			if retriesStr != "" {
				retries, err := strconv.Atoi(retriesStr)
				if err != nil {
					return nil, fmt.Errorf("invalid cron retries for job %s: %v", cronJob.Name, err)
				}
				if retries < 0 {
					return nil, fmt.Errorf("cron retries must be non-negative for job %s, got: %d", cronJob.Name, retries)
				}
				cronJob.Retries = retries
			}
		}

		if i < len(args.cronTimeouts) {
			timeoutStr := strings.TrimSpace(args.cronTimeouts[i])
			if timeoutStr != "" {
				timeout, err := time.ParseDuration(timeoutStr)
				if err != nil {
					return nil, fmt.Errorf("invalid cron timeout for job %s: %v", cronJob.Name, err)
				}
				if timeout <= 0 {
					return nil, fmt.Errorf("cron timeout must be positive for job %s", cronJob.Name)
				}
				cronJob.Timeout = timeout
			}
		}
	}

	return config, nil
}

