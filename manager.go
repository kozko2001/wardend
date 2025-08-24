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
	Config           ProcessConfig
	State            ProcessState
	Cmd              *exec.Cmd
	StartupAttempts  int       // Failures during startup phase
	RuntimeRestarts  int       // Restarts after successful startup
	LastStart        time.Time
	StartupComplete  bool      // Has process run long enough to be considered started
	mu               sync.RWMutex
}

type Manager struct {
	config    *Config
	processes map[string]*Process
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.RWMutex
}

func NewManager(config *Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(config.LogLevel),
	}))
	if config.LogFormat == LogFormatJSON {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: getLogLevel(config.LogLevel),
		}))
	}

	return &Manager{
		config:    config,
		processes: make(map[string]*Process),
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
	}
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

	m.logger.Info("starting process", "name", name, "command", process.Config.Command)
	
	process.State = StateStarting
	process.LastStart = time.Now()

	cmd := exec.CommandContext(m.ctx, "sh", "-c", process.Config.Command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

	m.logger.Info("process started", "name", name, "pid", cmd.Process.Pid)
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

	m.logger.Info("stopping process", "name", name)
	process.State = StateStopping

	if process.Cmd != nil && process.Cmd.Process != nil {
		if err := process.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			m.logger.Warn("failed to send SIGTERM", "name", name, "error", err)
		}

		done := make(chan error, 1)
		go func() {
			done <- process.Cmd.Wait()
		}()

		select {
		case <-time.After(m.config.ShutdownTimeout):
			m.logger.Warn("process did not stop gracefully, sending SIGKILL", "name", name)
			if err := process.Cmd.Process.Kill(); err != nil {
				m.logger.Error("failed to kill process", "name", name, "error", err)
			}
			<-done
		case err := <-done:
			if err != nil {
				m.logger.Debug("process exited with error", "name", name, "error", err)
			}
		}
	}

	process.State = StateStopped
	process.Cmd = nil
	m.logger.Info("process stopped", "name", name)
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
			m.logger.Debug("process startup completed", 
				"name", process.Config.Name, 
				"duration", time.Since(startTime))
		}
		process.mu.Unlock()
	}
}

func (m *Manager) StopAll() error {
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
		m.logger.Warn("process exited with error", 
			"name", process.Config.Name, 
			"exit_code", exitCode, 
			"error", err)
	} else {
		process.State = StateStopped
		m.logger.Info("process exited normally", "name", process.Config.Name)
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
		if !startupComplete {
			m.logger.Info("restarting process (startup failure)", 
				"name", processName, 
				"startup_attempt", startupAttempts,
				"max_retries", process.Config.StartRetries)
		} else {
			m.logger.Info("restarting process (runtime failure)", 
				"name", processName, 
				"runtime_restart", runtimeRestarts,
				"max_restarts", process.Config.MaxRestarts)
		}

		time.Sleep(restartDelay)

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := m.StartProcess(processName); err != nil {
				m.logger.Error("failed to restart process", 
					"name", processName, 
					"error", err)
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
		if process.StartupAttempts >= process.Config.StartRetries {
			m.logger.Error("process exceeded max startup attempts", 
				"name", process.Config.Name, 
				"startup_attempts", process.StartupAttempts,
				"max_start_retries", process.Config.StartRetries)
			return false
		}
		
		m.logger.Info("process failed during startup phase", 
			"name", process.Config.Name, 
			"attempt", process.StartupAttempts+1,
			"max_retries", process.Config.StartRetries)
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
	if process.RuntimeRestarts >= process.Config.MaxRestarts {
		m.logger.Error("process exceeded max runtime restart attempts", 
			"name", process.Config.Name, 
			"runtime_restarts", process.RuntimeRestarts,
			"max_restarts", process.Config.MaxRestarts)
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

	m.logger.Debug("waiting for dependencies", "process", processName, "dependencies", process.Config.DependsOn)

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