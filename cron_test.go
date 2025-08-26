package main

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestCronScheduleParsingHumanReadable tests human-readable schedule formats
func TestCronScheduleParsingHumanReadable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))

	scheduler := &CronScheduler{
		logger: logger,
	}

	tests := []struct {
		schedule string
		valid    bool
	}{
		{"daily", true},
		{"@daily", true},
		{"hourly", true},
		{"@hourly", true},
		{"weekly", true},
		{"monthly", true},
		{"yearly", true},
		{"every 5m", true},
		{"every 1h", true},
		{"every 30s", true},
		{"@every 2h", true},
		{"0 2 * * *", true},
		{"*/5 * * * *", false}, // Not implemented yet
		{"invalid", false},
		{"every", false},
		{"every xyz", false},
	}

	for _, test := range tests {
		t.Run(test.schedule, func(t *testing.T) {
			_, err := scheduler.parseScheduleToNextRun(test.schedule, time.Now())
			
			if test.valid && err != nil {
				t.Errorf("Expected schedule '%s' to be valid, got error: %v", test.schedule, err)
			}
			
			if !test.valid && err == nil {
				t.Errorf("Expected schedule '%s' to be invalid, but it was accepted", test.schedule)
			}
		})
	}
}

// TestCronConfigValidation tests cron job configuration validation
func TestCronConfigValidation(t *testing.T) {
	config := NewConfig()

	// Valid cron job
	cronJob1 := config.AddCronJob("test-job", "daily", "echo 'hello'")
	if cronJob1.Name != "test-job" {
		t.Errorf("Expected name 'test-job', got '%s'", cronJob1.Name)
	}
	if cronJob1.Schedule != "daily" {
		t.Errorf("Expected schedule 'daily', got '%s'", cronJob1.Schedule)
	}
	if cronJob1.Command != "echo 'hello'" {
		t.Errorf("Expected command 'echo 'hello'', got '%s'", cronJob1.Command)
	}

	// Auto-generated name
	cronJob2 := config.AddCronJob("", "hourly", "echo 'test'")
	if cronJob2.Name != "cron-2" {
		t.Errorf("Expected auto-generated name 'cron-2', got '%s'", cronJob2.Name)
	}

	// Test validation
	err := config.Validate()
	if err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Test duplicate name validation
	config.AddCronJob("test-job", "daily", "echo 'duplicate'")
	err = config.Validate()
	if err == nil {
		t.Error("Expected duplicate name error, but validation passed")
	}
}

// TestYAMLCronConfig tests YAML configuration parsing for cron jobs
func TestYAMLCronConfig(t *testing.T) {
	yamlCron := YAMLCronConfig{
		Name:      "test",
		Schedule:  "daily", 
		Command:   "echo test",
		Retries:   5,
		Timeout:   "5m",
		LogOutput: false,
	}

	cronConfig, err := yamlCron.ToCronConfig(1)
	if err != nil {
		t.Fatalf("Failed to convert YAML cron config: %v", err)
	}

	if cronConfig.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", cronConfig.Name)
	}
	if cronConfig.Schedule != "daily" {
		t.Errorf("Expected schedule 'daily', got '%s'", cronConfig.Schedule)
	}
	if cronConfig.Command != "echo test" {
		t.Errorf("Expected command 'echo test', got '%s'", cronConfig.Command)
	}
	if cronConfig.Retries != 5 {
		t.Errorf("Expected retries 5, got %d", cronConfig.Retries)
	}
	if cronConfig.Timeout != 5*time.Minute {
		t.Errorf("Expected timeout 5m, got %v", cronConfig.Timeout)
	}
	if cronConfig.LogOutput != false {
		t.Errorf("Expected log output false, got %v", cronConfig.LogOutput)
	}

	// Test auto-generated name
	yamlCron.Name = ""
	cronConfig, err = yamlCron.ToCronConfig(3)
	if err != nil {
		t.Fatalf("Failed to convert YAML cron config: %v", err)
	}
	if cronConfig.Name != "cron-3" {
		t.Errorf("Expected auto-generated name 'cron-3', got '%s'", cronConfig.Name)
	}
}

// TestCronSchedulerCreation tests basic cron scheduler creation
func TestCronSchedulerCreation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))

	cronConfigs := []CronConfig{
		{
			Name:      "test1",
			Schedule:  "daily",
			Command:   "echo test1",
			Retries:   3,
			Timeout:   10 * time.Minute,
			LogOutput: true,
		},
		{
			Name:      "test2",
			Schedule:  "every 30m",
			Command:   "echo test2",
			Retries:   2,
			Timeout:   5 * time.Minute,
			LogOutput: false,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	if scheduler == nil {
		t.Fatal("Scheduler is nil")
	}

	// Verify jobs were created
	jobs := scheduler.GetAllJobs()
	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs, got %d", len(jobs))
	}

	job1 := scheduler.GetJob("test1")
	if job1 == nil {
		t.Error("Job 'test1' not found")
	}

	job2 := scheduler.GetJob("test2")
	if job2 == nil {
		t.Error("Job 'test2' not found")
	}

	// Test with invalid schedule
	invalidConfigs := []CronConfig{
		{
			Name:     "invalid",
			Schedule: "invalid-schedule",
			Command:  "echo test",
			Retries:  1,
			Timeout:  time.Minute,
		},
	}

	_, err = NewCronScheduler(invalidConfigs, logger, nil)
	if err == nil {
		t.Error("Expected error for invalid schedule, but creation succeeded")
	}
}

// TestCronNextRunCalculation tests next run time calculations
func TestCronNextRunCalculation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	scheduler := &CronScheduler{
		logger: logger,
	}

	now := time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC) // Monday, 2:30 PM

	tests := []struct {
		schedule string
		testTime time.Time
		validate func(next time.Time) bool
	}{
		{
			schedule: "daily",
			testTime: now,
			validate: func(next time.Time) bool {
				// Should be next day at midnight
				expected := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
				return next.Equal(expected)
			},
		},
		{
			schedule: "hourly", 
			testTime: now,
			validate: func(next time.Time) bool {
				// Should be next hour at top of hour
				expected := time.Date(2024, 1, 15, 15, 0, 0, 0, time.UTC)
				return next.Equal(expected)
			},
		},
		{
			schedule: "every 2h",
			testTime: now,
			validate: func(next time.Time) bool {
				// Should be 2 hours from now
				expected := now.Add(2 * time.Hour)
				return next.Equal(expected)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.schedule, func(t *testing.T) {
			// Mock time.Now() by using calculateNextRun which takes a time parameter
			next := scheduler.calculateNextRun(test.schedule, test.testTime)
			
			if !test.validate(next) {
				t.Errorf("Next run time validation failed for schedule '%s': got %v", test.schedule, next)
			}
		})
	}
}

// TestCronJobExecution tests actual cron job execution with mocked commands
func TestCronJobExecution(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))

	// Test successful execution
	cronConfigs := []CronConfig{
		{
			Name:      "success-job",
			Schedule:  "every 1s",
			Command:   "echo 'test successful'",
			Retries:   1,
			Timeout:   5 * time.Second,
			LogOutput: false,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Wait for at least one execution
	time.Sleep(2500 * time.Millisecond) // Increased wait time

	// Check job status before stopping
	job := scheduler.GetJob("success-job")
	if job == nil {
		t.Fatal("Job not found")
	}

	job.mu.RLock()
	runCount := job.RunCount
	successCount := job.SuccessCount
	job.mu.RUnlock()

	// Wait a bit more if job is currently running to let it complete
	if runCount > successCount {
		time.Sleep(500 * time.Millisecond)
		job.mu.RLock()
		runCount = job.RunCount
		successCount = job.SuccessCount
		job.mu.RUnlock()
	}

	if runCount == 0 {
		t.Errorf("Job should have run at least once (runCount=%d, successCount=%d)", runCount, successCount)
	}
	if successCount == 0 {
		t.Errorf("Job should have succeeded at least once (runCount=%d, successCount=%d)", runCount, successCount)
	}
}

// TestCronJobRetryLogic tests retry behavior on failures
func TestCronJobRetryLogic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Test job that always fails
	cronConfigs := []CronConfig{
		{
			Name:      "fail-job",
			Schedule:  "every 1s",
			Command:   "exit 1", // Always fails
			Retries:   2,        // Will try 3 times total (initial + 2 retries)
			Timeout:   2 * time.Second,
			LogOutput: false,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	scheduler.Start()
	defer scheduler.Stop()

	// Wait for execution to complete (job has 2 retries with backoff delay)
	time.Sleep(4 * time.Second)

	// Wait until job is no longer running (gives it time to complete final attempt)
	var status map[string]interface{}
	for i := 0; i < 10; i++ { // Try up to 10 times (5 seconds)
		status = scheduler.GetJobStatus("fail-job")
		if status == nil {
			t.Fatal("Job status not available")
		}
		if state, ok := status["state"]; ok && state != "running" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Check that the job tried the expected number of attempts
	if lastExec, ok := status["last_execution"]; ok {
		if execMap, ok := lastExec.(map[string]interface{}); ok {
			if attemptCount, ok := execMap["attempt_count"]; ok {
				// Should be 3 attempts (1 initial + 2 retries)
				if attemptCount != 3 {
					t.Errorf("Expected 3 attempts (1+2 retries), got %v", attemptCount)
				}
			}
		}
	}

	// Verify failure count increased
	failureCount := status["failure_count"]
	if failureCount == nil || failureCount == 0 {
		t.Error("Expected failure count > 0")
	}
}

// TestCronJobStateTransitions tests job state changes during execution
func TestCronJobStateTransitions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // Enable debug logging
	}))

	cronConfigs := []CronConfig{
		{
			Name:      "state-test",
			Schedule:  "every 1s", // Run more frequently
			Command:   "sleep 0.2", // Even shorter sleep
			Retries:   1,
			Timeout:   3 * time.Second,
			LogOutput: false,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	scheduler.Start()
	defer scheduler.Stop()

	job := scheduler.GetJob("state-test")
	if job == nil {
		t.Fatal("Job not found")
	}

	// Initially should be idle
	job.mu.RLock()
	initialState := job.State
	job.mu.RUnlock()

	if initialState != CronStateIdle {
		t.Errorf("Expected initial state to be idle, got %s", initialState)
	}

	// Wait for execution to start
	time.Sleep(3000 * time.Millisecond) // Increased wait time

	// Should have transitioned through states
	job.mu.RLock()
	finalState := job.State
	runCount := job.RunCount
	successCount := job.SuccessCount
	job.mu.RUnlock()

	// Wait a bit more if job is currently running to let it complete
	if runCount > successCount {
		time.Sleep(500 * time.Millisecond)
		job.mu.RLock()
		finalState = job.State
		runCount = job.RunCount
		job.mu.RUnlock()
	}

	if runCount == 0 {
		t.Errorf("Job should have executed at least once (runCount=%d)", runCount)
	}

	// Should be back to idle after successful completion
	if finalState != CronStateIdle {
		t.Errorf("Expected final state to be idle, got %s", finalState)
	}
}

// TestCronJobStatus tests status reporting functionality
func TestCronJobStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cronConfigs := []CronConfig{
		{
			Name:      "status-test",
			Schedule:  "daily",
			Command:   "echo 'status test'",
			Retries:   3,
			Timeout:   1 * time.Minute,
			LogOutput: true,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	// Test status reporting
	status := scheduler.GetJobStatus("status-test")
	if status == nil {
		t.Fatal("Expected job status, got nil")
	}

	// Verify required fields exist
	requiredFields := []string{"name", "schedule", "command", "state", "next_run", "run_count", "success_count", "failure_count"}
	for _, field := range requiredFields {
		if _, exists := status[field]; !exists {
			t.Errorf("Status missing required field: %s", field)
		}
	}

	// Verify field values
	if status["name"] != "status-test" {
		t.Errorf("Expected name 'status-test', got %v", status["name"])
	}
	if status["schedule"] != "daily" {
		t.Errorf("Expected schedule 'daily', got %v", status["schedule"])
	}
	if status["command"] != "echo 'status test'" {
		t.Errorf("Expected command 'echo 'status test'', got %v", status["command"])
	}
	if status["state"] != string(CronStateIdle) {
		t.Errorf("Expected state 'idle', got %v", status["state"])
	}

	// Test all jobs status
	allStatuses := scheduler.GetAllJobStatuses()
	if len(allStatuses) != 1 {
		t.Errorf("Expected 1 job status, got %d", len(allStatuses))
	}
	if _, exists := allStatuses["status-test"]; !exists {
		t.Error("Expected 'status-test' in all statuses")
	}
}

// TestCronJobTimeout tests command timeout functionality
func TestCronJobTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cronConfigs := []CronConfig{
		{
			Name:      "timeout-test",
			Schedule:  "every 3s",
			Command:   "sleep 10", // Long sleep that should timeout
			Retries:   1,
			Timeout:   1 * time.Second, // Short timeout
			LogOutput: false,
		},
	}

	scheduler, err := NewCronScheduler(cronConfigs, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create cron scheduler: %v", err)
	}

	scheduler.Start()
	defer scheduler.Stop()

	// Wait for execution and timeout
	time.Sleep(5 * time.Second)

	// Check that the job failed due to timeout
	status := scheduler.GetJobStatus("timeout-test")
	if status == nil {
		t.Fatal("Job status not available")
	}

	failureCount := status["failure_count"]
	if failureCount == nil || failureCount == 0 {
		t.Error("Expected failure count > 0 due to timeout")
	}

	// Check that last execution had an error
	if lastExec, ok := status["last_execution"]; ok {
		if execMap, ok := lastExec.(map[string]interface{}); ok {
			if errorField, ok := execMap["error"]; ok {
				errorStr := errorField.(string)
				// Should contain context deadline exceeded or similar timeout error
				if errorStr == "" {
					t.Error("Expected timeout error in last execution")
				}
				t.Logf("Timeout error (expected): %s", errorStr)
			}
		}
	}
}