package main

import (
	"context"
	"testing"
	"time"
)

func TestManager_Initialize(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "test1", Command: "echo hello", RestartPolicy: RestartAlways},
			{Name: "test2", Command: "echo world", RestartPolicy: RestartNever},
		},
		LogFormat: LogFormatText,
		LogLevel:  LogLevelInfo,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if len(manager.processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(manager.processes))
	}

	if _, exists := manager.processes["test1"]; !exists {
		t.Error("Process test1 not found")
	}

	if _, exists := manager.processes["test2"]; !exists {
		t.Error("Process test2 not found")
	}
}

func TestManager_StartStopProcess(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "sleep_test", Command: "sleep 10", RestartPolicy: RestartNever, StartRetries: 3, StartupTime: 1 * time.Second, MaxRestarts: 5, RestartDelay: 100 * time.Millisecond},
		},
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelInfo,
		ShutdownTimeout: 2 * time.Second,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := manager.StartProcess("sleep_test"); err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	state, err := manager.GetProcessState("sleep_test")
	if err != nil {
		t.Fatalf("GetProcessState failed: %v", err)
	}

	if state != StateRunning {
		t.Errorf("Expected process state to be running, got %s", state)
	}

	if err := manager.StopProcess("sleep_test"); err != nil {
		t.Fatalf("StopProcess failed: %v", err)
	}

	// Wait for process to actually stop (it may take up to ShutdownTimeout)
	timeout := time.Now().Add(3 * time.Second)
	for time.Now().Before(timeout) {
		state, err = manager.GetProcessState("sleep_test")
		if err != nil {
			t.Fatalf("GetProcessState failed: %v", err)
		}
		if state == StateStopped {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if state != StateStopped {
		t.Errorf("Expected process state to be stopped, got %s", state)
	}

	manager.cancel()
	time.Sleep(100 * time.Millisecond) // Wait for cleanup
}

func TestManager_DependencyResolution(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "app", Command: "echo app", RestartPolicy: RestartNever, DependsOn: []string{"db"}},
			{Name: "db", Command: "echo db", RestartPolicy: RestartNever, DependsOn: []string{}},
			{Name: "worker", Command: "echo worker", RestartPolicy: RestartNever, DependsOn: []string{"db", "app"}},
		},
		LogFormat: LogFormatText,
		LogLevel:  LogLevelInfo,
	}

	manager := NewManager(config)

	startOrder, err := manager.getStartOrder()
	if err != nil {
		t.Fatalf("getStartOrder failed: %v", err)
	}

	if len(startOrder) != 3 {
		t.Errorf("Expected 3 processes in start order, got %d", len(startOrder))
	}

	if startOrder[0] != "db" {
		t.Errorf("Expected db to be first, got %s", startOrder[0])
	}

	if startOrder[1] != "app" {
		t.Errorf("Expected app to be second, got %s", startOrder[1])
	}

	if startOrder[2] != "worker" {
		t.Errorf("Expected worker to be third, got %s", startOrder[2])
	}

	stopOrder := manager.getStopOrder()
	if len(stopOrder) != 3 {
		t.Errorf("Expected 3 processes in stop order, got %d", len(stopOrder))
	}

	if stopOrder[0] != "worker" {
		t.Errorf("Expected worker to be stopped first, got %s", stopOrder[0])
	}

	if stopOrder[2] != "db" {
		t.Errorf("Expected db to be stopped last, got %s", stopOrder[2])
	}

	manager.cancel()
}

func TestManager_CircularDependencyDetection(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "app", Command: "echo app", RestartPolicy: RestartNever, DependsOn: []string{"worker"}},
			{Name: "worker", Command: "echo worker", RestartPolicy: RestartNever, DependsOn: []string{"app"}},
		},
		LogFormat: LogFormatText,
		LogLevel:  LogLevelInfo,
	}

	manager := NewManager(config)

	_, err := manager.getStartOrder()
	if err == nil {
		t.Error("Expected error for circular dependency, but got none")
	}

	manager.cancel()
}

func TestManager_RestartProcess(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "quick_task", 
				Command:       "echo hello", 
				RestartPolicy: RestartNever, 
				RestartDelay:  100 * time.Millisecond,
			},
		},
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelInfo,
		ShutdownTimeout: 2 * time.Second,
		RestartDelay:    100 * time.Millisecond,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := manager.StartProcess("quick_task"); err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if err := manager.RestartProcess("quick_task"); err != nil {
		t.Fatalf("RestartProcess failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	manager.cancel()
}

func TestManager_AllProcessStates(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "test1", Command: "sleep 1", RestartPolicy: RestartNever},
			{Name: "test2", Command: "sleep 1", RestartPolicy: RestartNever},
		},
		LogFormat: LogFormatText,
		LogLevel:  LogLevelInfo,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	states := manager.GetAllProcessStates()
	if len(states) != 2 {
		t.Errorf("Expected 2 process states, got %d", len(states))
	}

	for name, state := range states {
		if state != StateStopped {
			t.Errorf("Expected process %s to be stopped initially, got %s", name, state)
		}
	}

	manager.cancel()
}

func TestManager_StartAll_WithDependencies(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{Name: "db", Command: "echo db started; sleep 1", RestartPolicy: RestartNever, DependsOn: []string{}},
			{Name: "app", Command: "echo app started; sleep 1", RestartPolicy: RestartNever, DependsOn: []string{"db"}},
		},
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelInfo,
		ShutdownTimeout: 5 * time.Second,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- manager.StartAll()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StartAll failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("StartAll timed out")
	}

	time.Sleep(500 * time.Millisecond)

	states := manager.GetAllProcessStates()
	for name, state := range states {
		if state != StateRunning && state != StateStopped {
			t.Errorf("Unexpected state for process %s: %s", name, state)
		}
	}

	manager.cancel()
}

func TestManager_ProcessMonitoring_RestartOnFailure(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "failing_process",
				Command:       "exit 1",
				RestartPolicy: RestartOnFailure,
				StartRetries:  3,
				StartupTime:  1 * time.Second,
				MaxRestarts:   2,
				RestartDelay:  200 * time.Millisecond,
			},
		},
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelDebug,
		ShutdownTimeout: 2 * time.Second,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := manager.StartProcess("failing_process"); err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	time.Sleep(1 * time.Second)

	process := manager.processes["failing_process"]
	process.mu.RLock()
	startupAttempts := process.StartupAttempts
	runtimeRestarts := process.RuntimeRestarts
	process.mu.RUnlock()

	// Should have some restart attempts (could be startup or runtime)
	if startupAttempts == 0 && runtimeRestarts == 0 {
		t.Error("Expected process to have been restarted at least once")
	}

	manager.cancel()
}

func TestManager_ProcessMonitoring_NoRestartOnNever(t *testing.T) {
	config := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "quick_exit",
				Command:       "exit 0",
				RestartPolicy: RestartNever,
				StartRetries:  3,
				StartupTime:  1 * time.Second,
				MaxRestarts:   5,
				RestartDelay:  100 * time.Millisecond,
			},
		},
		LogFormat:       LogFormatText,
		LogLevel:        LogLevelDebug,
		ShutdownTimeout: 2 * time.Second,
	}

	manager := NewManager(config)
	
	if err := manager.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := manager.StartProcess("quick_exit"); err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	process := manager.processes["quick_exit"]
	process.mu.RLock()
	startupAttempts := process.StartupAttempts
	runtimeRestarts := process.RuntimeRestarts
	state := process.State
	process.mu.RUnlock()

	if startupAttempts != 0 || runtimeRestarts != 0 {
		t.Errorf("Expected no restarts for RestartNever policy, got startup=%d runtime=%d", startupAttempts, runtimeRestarts)
	}

	if state != StateStopped {
		t.Errorf("Expected process to be stopped, got %s", state)
	}

	manager.cancel()
}

// TestManager_TwoPhaseRestart tests the new startup vs runtime restart logic
func TestManager_TwoPhaseRestart(t *testing.T) {
	t.Run("StartupFailure", func(t *testing.T) {
		// Process that exits immediately (startup failure)
		config := &Config{
			Processes: []ProcessConfig{
				{
					Name: "startup_failure", 
					Command: "exit 1", 
					RestartPolicy: RestartAlways,
					StartRetries: 2,
					StartupTime: 1 * time.Second,
					MaxRestarts: 5,
					RestartDelay: 100 * time.Millisecond,
				},
			},
			LogFormat: LogFormatText,
			LogLevel:  LogLevelInfo,
		}

		manager := NewManager(config)
		if err := manager.Initialize(); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		// Start process and let it fail during startup phase
		if err := manager.StartProcess("startup_failure"); err != nil {
			t.Fatalf("StartProcess failed: %v", err)
		}

		// Wait for startup failures to exhaust
		time.Sleep(500 * time.Millisecond)

		process := manager.processes["startup_failure"]
		process.mu.RLock()
		startupAttempts := process.StartupAttempts
		runtimeRestarts := process.RuntimeRestarts
		startupComplete := process.StartupComplete
		process.mu.RUnlock()

		// Should have failed startup attempts but no runtime restarts
		if startupAttempts < 2 {
			t.Errorf("Expected at least 2 startup attempts, got %d", startupAttempts)
		}
		if runtimeRestarts > 0 {
			t.Errorf("Expected 0 runtime restarts, got %d", runtimeRestarts)
		}
		if startupComplete {
			t.Errorf("Process should not have completed startup")
		}

		manager.cancel()
	})

	t.Run("RuntimeFailure", func(t *testing.T) {
		// Process that runs for a while then exits (runtime failure)
		config := &Config{
			Processes: []ProcessConfig{
				{
					Name: "runtime_failure", 
					Command: "sleep 2; exit 1", 
					RestartPolicy: RestartAlways,
					StartRetries: 3,
					StartupTime: 500 * time.Millisecond, // Short startup window
					MaxRestarts: 2,
					RestartDelay: 100 * time.Millisecond,
				},
			},
			LogFormat: LogFormatText,
			LogLevel:  LogLevelInfo,
		}

		manager := NewManager(config)
		if err := manager.Initialize(); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		if err := manager.StartProcess("runtime_failure"); err != nil {
			t.Fatalf("StartProcess failed: %v", err)
		}

		// Wait for startup completion + first runtime failure + restart + second failure
		time.Sleep(7 * time.Second)

		process := manager.processes["runtime_failure"]
		process.mu.RLock()
		startupAttempts := process.StartupAttempts
		runtimeRestarts := process.RuntimeRestarts
		process.mu.RUnlock()

		// Should have minimal startup attempts but multiple runtime restarts
		if startupAttempts > 1 {
			t.Errorf("Expected minimal startup attempts, got %d", startupAttempts)
		}
		if runtimeRestarts < 2 {
			t.Errorf("Expected at least 2 runtime restarts, got %d", runtimeRestarts)
		}

		manager.cancel()
	})

	t.Run("InfiniteRuntimeRestarts", func(t *testing.T) {
		// Test infinite runtime restarts
		config := &Config{
			Processes: []ProcessConfig{
				{
					Name: "infinite_restarts", 
					Command: "sleep 1; exit 0", 
					RestartPolicy: RestartAlways,
					StartRetries: 3,
					StartupTime: 500 * time.Millisecond,
					MaxRestarts: -1, // infinite
					RestartDelay: 100 * time.Millisecond,
				},
			},
			LogFormat: LogFormatText,
			LogLevel:  LogLevelInfo,
		}

		manager := NewManager(config)
		if err := manager.Initialize(); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		if err := manager.StartProcess("infinite_restarts"); err != nil {
			t.Fatalf("StartProcess failed: %v", err)
		}

		// Wait for multiple runtime restarts 
		time.Sleep(4 * time.Second)

		process := manager.processes["infinite_restarts"]
		process.mu.RLock()
		runtimeRestarts := process.RuntimeRestarts
		state := process.State
		process.mu.RUnlock()

		// Should keep restarting with no limit
		if runtimeRestarts < 3 {
			t.Errorf("Expected multiple runtime restarts, got %d", runtimeRestarts)
		}
		if state == StateFailed {
			t.Errorf("Process should not be in failed state with infinite restarts")
		}

		manager.cancel()
	})
}

// TestManager_ParseMaxRestarts tests the new max restarts parsing
func TestManager_ParseMaxRestarts(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"infinite", -1, false},
		{"unlimited", -1, false},
		{"5", 5, false},
		{"0", 0, false},
		{"-2", 0, true},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, test := range tests {
		result, err := ParseMaxRestarts(test.input)
		
		if test.hasError && err == nil {
			t.Errorf("Expected error for input %q, but got none", test.input)
		}
		if !test.hasError && err != nil {
			t.Errorf("Unexpected error for input %q: %v", test.input, err)
		}
		if !test.hasError && result != test.expected {
			t.Errorf("For input %q, expected %d, got %d", test.input, test.expected, result)
		}
	}
}