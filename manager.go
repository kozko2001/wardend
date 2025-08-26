package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type ProcessState string

const (
	StateStopped  ProcessState = "stopped"
	StateStarting ProcessState = "starting"
	StateRunning  ProcessState = "running"
	StateStopping ProcessState = "stopping"
	StateFailed   ProcessState = "failed"
)

type Process struct {
	Config          ProcessConfig
	State           ProcessState
	Cmd             *exec.Cmd
	StartupAttempts int // Failures during startup phase
	RuntimeRestarts int // Restarts after successful startup
	LastStart       time.Time
	StartupComplete bool // Has process run long enough to be considered started
	mu              sync.RWMutex
}

type Manager struct {
	config        *Config
	processes     map[string]*Process
	cronScheduler *CronScheduler
	logger        *slog.Logger
	logManager    *LogManager
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.RWMutex
	healthChecker *HealthChecker
	httpServer    *HTTPServer
}

func NewManager(config *Config) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(config.LogLevel),
	}))
	if config.LogFormat == LogFormatJSON {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: getLogLevel(config.LogLevel),
		}))
	}

	manager := &Manager{
		config:     config,
		processes:  make(map[string]*Process),
		logger:     logger,
		logManager: NewLogManager(config.LogDir, logger),
		ctx:        ctx,
		cancel:     cancel,
	}

	manager.healthChecker = NewHealthChecker(manager)

	// Initialize cron scheduler if cron jobs are configured
	if len(config.CronJobs) > 0 {
		cronScheduler, err := NewCronScheduler(config.CronJobs, logger, manager.logManager)
		if err != nil {
			logger.Error("failed to create cron scheduler", "error", err)
			cancel()
			return nil, fmt.Errorf("failed to create cron scheduler: %v", err)
		}
		manager.cronScheduler = cronScheduler
	}

	// Initialize HTTP server if port is configured
	if config.HTTPPort > 0 {
		manager.httpServer = NewHTTPServer(manager, config.HTTPPort)
	}

	return manager, nil
}

func getLogLevel(level LogLevel) slog.Level {
	switch level {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (m *Manager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, processConfig := range m.config.Processes {
		process := &Process{
			Config: processConfig,
			State:  StateStopped,
		}
		m.processes[processConfig.Name] = process
		m.logger.Debug("initialized process", "name", processConfig.Name)
	}

	return nil
}

func (m *Manager) StartAll() error {
	startOrder, err := m.getStartOrder()
	if err != nil {
		return fmt.Errorf("failed to determine start order: %v", err)
	}

	m.logger.Info("starting processes", "order", startOrder)

	for _, processName := range startOrder {
		if err := m.StartProcess(processName); err != nil {
			return fmt.Errorf("failed to start process %s: %v", processName, err)
		}

		if err := m.waitForDependencies(processName); err != nil {
			return fmt.Errorf("dependency check failed for process %s: %v", processName, err)
		}
	}

	// Start health checker after all processes are started
	m.healthChecker.Start()

	// Start cron scheduler if configured
	if m.cronScheduler != nil {
		m.cronScheduler.Start()
		m.logger.Info("cron scheduler started")
	}

	// Start HTTP server if configured
	if m.httpServer != nil {
		if err := m.httpServer.Start(); err != nil {
			return fmt.Errorf("failed to start HTTP server: %v", err)
		}
	}

	return nil
}

func (m *Manager) StartProcess(name string) error {
	m.mu.RLock()
	process, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	process.mu.Lock()
	defer process.mu.Unlock()

	if process.State == StateRunning {
		return nil
	}

	processLogger := m.logger.With(
		"process", name,
		"command", process.Config.Command,
		"restart_policy", process.Config.RestartPolicy,
		"startup_attempts", process.StartupAttempts,
		"runtime_restarts", process.RuntimeRestarts,
	)

	processLogger.Info("starting process")

	process.State = StateStarting
	process.LastStart = time.Now()

	cmd := exec.CommandContext(m.ctx, "sh", "-c", process.Config.Command)

	// Set up per-process logging
	fileLogger, err := m.logManager.GetProcessLogger(name)
	if err != nil {
		process.State = StateFailed
		return fmt.Errorf("failed to get process logger: %v", err)
	}

	// Create writers that log to both process log and stdout/stderr
	if m.config.LogDir == "" {
		// No log directory, just use stdout/stderr with process prefixes
		cmd.Stdout = NewProcessWriter(name, os.Stdout, false)
		cmd.Stderr = NewProcessWriter(name, os.Stderr, true)
	} else {
		// Log to both process file and stdout/stderr
		cmd.Stdout = NewMultiWriter(
			fileLogger,
			NewProcessWriter(name, os.Stdout, false),
		)
		cmd.Stderr = NewMultiWriter(
			fileLogger,
			NewProcessWriter(name, os.Stderr, true),
		)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		process.State = StateFailed
		return fmt.Errorf("failed to start command: %v", err)
	}

	process.Cmd = cmd
	process.State = StateRunning
	process.StartupComplete = false

	m.wg.Add(1)
	go m.monitorProcess(process)

	// Start a goroutine to mark startup as complete after StartupTime
	m.wg.Add(1)
	go m.trackStartupCompletion(process)

	processLogger.Info("process started", "pid", cmd.Process.Pid, "state", StateRunning)
	return nil
}

func (m *Manager) StopProcess(name string) error {
	m.mu.RLock()
	process, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	process.mu.Lock()
	defer process.mu.Unlock()

	if process.State != StateRunning {
		return nil
	}

	processLogger := m.logger.With("process", name, "state", process.State)
	processLogger.Info("stopping process")
	process.State = StateStopping

	if process.Cmd != nil && process.Cmd.Process != nil {
		if err := process.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			processLogger.Warn("failed to send SIGTERM", "error", err, "signal", "SIGTERM")
		}

		done := make(chan error, 1)
		go func() {
			done <- process.Cmd.Wait()
		}()

		select {
		case <-time.After(m.config.ShutdownTimeout):
			processLogger.Warn("process did not stop gracefully, sending SIGKILL",
				"timeout", m.config.ShutdownTimeout,
				"signal", "SIGKILL")
			if err := process.Cmd.Process.Kill(); err != nil {
				processLogger.Error("failed to kill process", "error", err, "signal", "SIGKILL")
			}
			<-done
		case err := <-done:
			if err != nil {
				processLogger.Debug("process exited with error during shutdown", "error", err)
			}
		}
	}

	process.State = StateStopped
	process.Cmd = nil
	processLogger.Info("process stopped", "final_state", StateStopped)
	return nil
}

func (m *Manager) RestartProcess(name string) error {
	if err := m.StopProcess(name); err != nil {
		return fmt.Errorf("failed to stop process for restart: %v", err)
	}

	time.Sleep(m.config.RestartDelay)

	if err := m.StartProcess(name); err != nil {
		return fmt.Errorf("failed to start process after restart: %v", err)
	}

	return nil
}

func (m *Manager) trackStartupCompletion(process *Process) {
	defer m.wg.Done()

	startTime := time.Now()
	select {
	case <-m.ctx.Done():
		return
	case <-time.After(process.Config.StartupTime):
		// Check if process is still running after StartupTime
		process.mu.Lock()
		if process.State == StateRunning {
			process.StartupComplete = true
			startupLogger := m.logger.With(
				"process", process.Config.Name,
				"phase", "startup_completion",
				"startup_time", process.Config.StartupTime,
			)
			startupLogger.Debug("process startup completed",
				"duration", time.Since(startTime),
				"state", StateRunning)
		}
		process.mu.Unlock()
	}
}

func (m *Manager) StopAll() error {
	// Stop HTTP server first
	if m.httpServer != nil {
		if err := m.httpServer.Stop(); err != nil {
			m.logger.Warn("failed to stop HTTP server", "error", err)
		}
	}

	// Stop cron scheduler
	if m.cronScheduler != nil {
		m.cronScheduler.Stop()
		m.logger.Info("cron scheduler stopped")
	}

	// Stop health checker
	m.healthChecker.Stop()

	stopOrder := m.getStopOrder()
	m.logger.Info("stopping all processes", "order", stopOrder)

	var wg sync.WaitGroup
	errors := make(chan error, len(stopOrder))

	for _, processName := range stopOrder {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := m.StopProcess(name); err != nil {
				errors <- fmt.Errorf("failed to stop process %s: %v", name, err)
			}
		}(processName)
	}

	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping processes: %v", errs)
	}

	// Close log manager
	if err := m.logManager.CloseAll(); err != nil {
		m.logger.Warn("failed to close log manager", "error", err)
	}

	m.cancel()
	return nil
}

func (m *Manager) monitorProcess(process *Process) {
	defer m.wg.Done()

	// Get a reference to the command before waiting
	process.mu.RLock()
	cmd := process.Cmd
	process.mu.RUnlock()

	if cmd == nil {
		return
	}

	err := cmd.Wait()

	process.mu.Lock()
	currentState := process.State
	if currentState == StateStopping {
		process.State = StateStopped
		process.mu.Unlock()
		return
	}

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
		process.State = StateFailed
		processLogger := m.logger.With(
			"process", process.Config.Name,
			"state", StateFailed,
			"startup_complete", process.StartupComplete,
			"startup_attempts", process.StartupAttempts,
			"runtime_restarts", process.RuntimeRestarts,
		)
		processLogger.Warn("process exited with error",
			"exit_code", exitCode,
			"error", err,
			"uptime", time.Since(process.LastStart))
	} else {
		process.State = StateStopped
		processLogger := m.logger.With(
			"process", process.Config.Name,
			"state", StateStopped,
			"uptime", time.Since(process.LastStart),
		)
		processLogger.Info("process exited normally")
	}

	shouldRestart := m.shouldRestart(process, exitCode)

	// Increment appropriate counter based on startup phase
	if shouldRestart {
		if !process.StartupComplete {
			process.StartupAttempts++
		} else {
			process.RuntimeRestarts++
		}
	}

	processName := process.Config.Name
	startupAttempts := process.StartupAttempts
	runtimeRestarts := process.RuntimeRestarts
	restartDelay := process.Config.RestartDelay
	startupComplete := process.StartupComplete
	process.mu.Unlock()

	if shouldRestart {
		restartLogger := m.logger.With(
			"process", processName,
			"restart_policy", process.Config.RestartPolicy,
			"restart_delay", restartDelay,
		)

		if !startupComplete {
			restartLogger.Info("restarting process (startup failure)",
				"startup_attempt", startupAttempts,
				"max_retries", process.Config.StartRetries,
				"phase", "startup")
		} else {
			restartLogger.Info("restarting process (runtime failure)",
				"runtime_restart", runtimeRestarts,
				"max_restarts", process.Config.MaxRestarts,
				"phase", "runtime")
		}

		time.Sleep(restartDelay)

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := m.StartProcess(processName); err != nil {
				restartLogger.Error("failed to restart process", "error", err)
			}
		}()
	}
}

func (m *Manager) shouldRestart(process *Process, exitCode int) bool {
	if process.Config.RestartPolicy == RestartNever {
		return false
	}

	// Two-phase restart logic
	if !process.StartupComplete {
		// Phase 1: Startup failures
		phaseLogger := m.logger.With(
			"process", process.Config.Name,
			"phase", "startup",
			"startup_attempts", process.StartupAttempts,
			"max_start_retries", process.Config.StartRetries,
		)

		if process.StartupAttempts >= process.Config.StartRetries {
			phaseLogger.Error("process exceeded max startup attempts")
			return false
		}

		phaseLogger.Info("process failed during startup phase",
			"attempt", process.StartupAttempts+1)
		return true
	}

	// Phase 2: Runtime failures
	// Check if infinite restarts are allowed
	if process.Config.MaxRestarts == -1 {
		// Infinite restarts allowed
		if process.Config.RestartPolicy == RestartAlways {
			return true
		}
		if process.Config.RestartPolicy == RestartOnFailure && exitCode != 0 {
			return true
		}
		return false
	}

	// Limited runtime restarts
	runtimeLogger := m.logger.With(
		"process", process.Config.Name,
		"phase", "runtime",
		"runtime_restarts", process.RuntimeRestarts,
		"max_restarts", process.Config.MaxRestarts,
		"exit_code", exitCode,
	)

	if process.RuntimeRestarts >= process.Config.MaxRestarts {
		runtimeLogger.Error("process exceeded max runtime restart attempts")
		return false
	}

	if process.Config.RestartPolicy == RestartAlways {
		return true
	}

	if process.Config.RestartPolicy == RestartOnFailure && exitCode != 0 {
		return true
	}

	return false
}

func (m *Manager) waitForDependencies(processName string) error {
	process := m.processes[processName]
	if len(process.Config.DependsOn) == 0 {
		return nil
	}

	dependencyLogger := m.logger.With(
		"process", processName,
		"dependencies", process.Config.DependsOn,
		"dependency_count", len(process.Config.DependsOn),
	)
	dependencyLogger.Debug("waiting for dependencies")

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for dependencies of %s", processName)
		case <-ticker.C:
			allReady := true
			for _, depName := range process.Config.DependsOn {
				if dep, exists := m.processes[depName]; !exists || dep.State != StateRunning {
					allReady = false
					break
				}
			}
			if allReady {
				return nil
			}
		}
	}
}

func (m *Manager) GetProcessState(name string) (ProcessState, error) {
	m.mu.RLock()
	process, exists := m.processes[name]
	m.mu.RUnlock()

	if !exists {
		return StateStopped, fmt.Errorf("process %s not found", name)
	}

	process.mu.RLock()
	state := process.State
	process.mu.RUnlock()

	return state, nil
}

func (m *Manager) GetAllProcessStates() map[string]ProcessState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make(map[string]ProcessState)
	for name, process := range m.processes {
		process.mu.RLock()
		states[name] = process.State
		process.mu.RUnlock()
	}

	return states
}

func (m *Manager) getStartOrder() ([]string, error) {
	return m.topologicalSort(false)
}

func (m *Manager) getStopOrder() []string {
	startOrder, _ := m.topologicalSort(false)
	stopOrder := make([]string, len(startOrder))
	for i := 0; i < len(startOrder); i++ {
		stopOrder[i] = startOrder[len(startOrder)-1-i]
	}
	return stopOrder
}

func (m *Manager) topologicalSort(reverse bool) ([]string, error) {
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	for _, process := range m.config.Processes {
		inDegree[process.Name] = 0
		graph[process.Name] = []string{}
	}

	for _, process := range m.config.Processes {
		for _, dep := range process.DependsOn {
			if reverse {
				graph[process.Name] = append(graph[process.Name], dep)
				inDegree[dep]++
			} else {
				graph[dep] = append(graph[dep], process.Name)
				inDegree[process.Name]++
			}
		}
	}

	var queue []string
	for processName, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, processName)
		}
	}

	var result []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(m.config.Processes) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

// GetHealthStatus returns the health status for a specific process
func (m *Manager) GetHealthStatus(processName string) *HealthCheckResult {
	return m.healthChecker.GetHealthStatus(processName)
}

// GetAllHealthStatuses returns health status for all processes
func (m *Manager) GetAllHealthStatuses() map[string]*HealthCheckResult {
	return m.healthChecker.GetAllHealthStatuses()
}

// GetHealthSummary returns an overall health summary
func (m *Manager) GetHealthSummary() map[string]interface{} {
	return m.healthChecker.GetHealthSummary()
}

// IsOverallHealthy returns true if all processes with health checks are healthy
func (m *Manager) IsOverallHealthy() bool {
	return m.healthChecker.IsOverallHealthy()
}

// GetCronJob returns a cron job by name
func (m *Manager) GetCronJob(name string) *CronJob {
	if m.cronScheduler == nil {
		return nil
	}
	return m.cronScheduler.GetJob(name)
}

// GetAllCronJobs returns all cron jobs
func (m *Manager) GetAllCronJobs() map[string]*CronJob {
	if m.cronScheduler == nil {
		return make(map[string]*CronJob)
	}
	return m.cronScheduler.GetAllJobs()
}

// GetCronJobStatus returns the status of a cron job
func (m *Manager) GetCronJobStatus(name string) map[string]interface{} {
	if m.cronScheduler == nil {
		return nil
	}
	return m.cronScheduler.GetJobStatus(name)
}

// GetAllCronJobStatuses returns the status of all cron jobs
func (m *Manager) GetAllCronJobStatuses() map[string]interface{} {
	if m.cronScheduler == nil {
		return make(map[string]interface{})
	}
	return m.cronScheduler.GetAllJobStatuses()
}
