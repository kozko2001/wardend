package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronJobState represents the state of a cron job execution
type CronJobState string

const (
	CronStateIdle    CronJobState = "idle"
	CronStateRunning CronJobState = "running"
	CronStateFailed  CronJobState = "failed"
)

// CronExecution represents a single execution of a cron job
type CronExecution struct {
	StartTime    time.Time
	EndTime      *time.Time
	ExitCode     int
	Error        error
	AttemptCount int
}

// CronJob represents a scheduled job with its runtime state
type CronJob struct {
	Config        CronConfig
	State         CronJobState
	NextRun       time.Time
	LastExecution *CronExecution
	RunCount      int64
	SuccessCount  int64
	FailureCount  int64
	mu            sync.RWMutex
}

// CronScheduler manages all cron jobs
type CronScheduler struct {
	jobs      map[string]*CronJob
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.RWMutex
	ticker    *time.Ticker
	logManager *LogManager
}

// NewCronScheduler creates a new cron scheduler
func NewCronScheduler(cronConfigs []CronConfig, logger *slog.Logger, logManager *LogManager) (*CronScheduler, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	scheduler := &CronScheduler{
		jobs:       make(map[string]*CronJob),
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		ticker:     time.NewTicker(1 * time.Second), // Check every second for better precision
		logManager: logManager,
	}

	// Initialize cron jobs
	for _, config := range cronConfigs {
		cronJob, err := scheduler.createCronJob(config)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create cron job '%s': %v", config.Name, err)
		}
		scheduler.jobs[config.Name] = cronJob
	}

	return scheduler, nil
}

// createCronJob creates a new CronJob from config
func (cs *CronScheduler) createCronJob(config CronConfig) (*CronJob, error) {
	nextRun, err := cs.parseScheduleToNextRun(config.Schedule, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid schedule '%s': %v", config.Schedule, err)
	}

	return &CronJob{
		Config:  config,
		State:   CronStateIdle,
		NextRun: nextRun,
	}, nil
}

// parseScheduleToNextRun parses various schedule formats and returns the next run time
func (cs *CronScheduler) parseScheduleToNextRun(schedule string, from time.Time) (time.Time, error) {
	schedule = strings.TrimSpace(strings.ToLower(schedule))
	now := from

	// Handle human-readable aliases
	switch schedule {
	case "daily", "@daily":
		return cs.nextDailyRun(now), nil
	case "hourly", "@hourly":
		return cs.nextHourlyRun(now), nil
	case "weekly", "@weekly":
		return cs.nextWeeklyRun(now), nil
	case "monthly", "@monthly":
		return cs.nextMonthlyRun(now), nil
	case "yearly", "annually", "@yearly", "@annually":
		return cs.nextYearlyRun(now), nil
	}

	// Handle interval formats (every X)
	if strings.HasPrefix(schedule, "every ") || strings.HasPrefix(schedule, "@every ") {
		intervalStr := strings.TrimPrefix(schedule, "every ")
		intervalStr = strings.TrimPrefix(intervalStr, "@every ")
		intervalStr = strings.TrimSpace(intervalStr)
		
		duration, err := time.ParseDuration(intervalStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid interval '%s': %v", intervalStr, err)
		}
		
		if duration < time.Second {
			return time.Time{}, fmt.Errorf("interval too small: %v (minimum 1 second)", duration)
		}
		
		return now.Add(duration), nil
	}

	// Handle traditional cron expressions (5 fields: minute hour day month weekday)
	return cs.parseCronExpression(schedule, now)
}

// nextDailyRun returns next run time for daily schedule (midnight)
func (cs *CronScheduler) nextDailyRun(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return next
}

// nextHourlyRun returns next run time for hourly schedule (top of next hour)
func (cs *CronScheduler) nextHourlyRun(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	return next
}

// nextWeeklyRun returns next run time for weekly schedule (Sunday midnight)
func (cs *CronScheduler) nextWeeklyRun(now time.Time) time.Time {
	daysUntilSunday := (7 - int(now.Weekday())) % 7
	if daysUntilSunday == 0 && (now.Hour() > 0 || now.Minute() > 0 || now.Second() > 0) {
		daysUntilSunday = 7
	}
	next := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 0, 0, 0, 0, now.Location())
	return next
}

// nextMonthlyRun returns next run time for monthly schedule (1st of next month)
func (cs *CronScheduler) nextMonthlyRun(now time.Time) time.Time {
	if now.Day() == 1 && now.Hour() == 0 && now.Minute() == 0 && now.Second() == 0 {
		// If it's exactly the 1st at midnight, schedule for next month
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	}
	// Otherwise, schedule for the 1st of next month
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
}

// nextYearlyRun returns next run time for yearly schedule (Jan 1st)
func (cs *CronScheduler) nextYearlyRun(now time.Time) time.Time {
	if now.Month() == time.January && now.Day() == 1 && now.Hour() == 0 && now.Minute() == 0 && now.Second() == 0 {
		return time.Date(now.Year()+1, time.January, 1, 0, 0, 0, 0, now.Location())
	}
	return time.Date(now.Year()+1, time.January, 1, 0, 0, 0, 0, now.Location())
}

// parseCronExpression parses traditional cron expressions (minute hour day month weekday)
func (cs *CronScheduler) parseCronExpression(expression string, now time.Time) (time.Time, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have 5 fields (minute hour day month weekday), got %d", len(fields))
	}

	// Basic cron parsing - this is a simplified implementation
	// For production use, consider using a proper cron parsing library
	minute, err := cs.parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid minute field: %v", err)
	}

	hour, err := cs.parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour field: %v", err)
	}

	// For simplicity, we'll calculate the next run time based on minute and hour
	// A full implementation would handle day, month, and weekday fields
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	
	// If the time has already passed today, schedule for tomorrow
	if next.Before(now) || next.Equal(now) {
		next = next.Add(24 * time.Hour)
	}

	return next, nil
}

// parseCronField parses a single cron field (supports *, numbers, and basic ranges)
func (cs *CronScheduler) parseCronField(field string, min, max int) (int, error) {
	if field == "*" {
		return min, nil
	}

	// Handle simple numbers
	if val, err := strconv.Atoi(field); err == nil {
		if val < min || val > max {
			return 0, fmt.Errorf("value %d out of range [%d-%d]", val, min, max)
		}
		return val, nil
	}

	// For now, we only support * and numbers
	// A full implementation would support ranges (1-5), lists (1,3,5), and steps (*/5)
	return 0, fmt.Errorf("unsupported cron field format: %s", field)
}

// Start starts the cron scheduler
func (cs *CronScheduler) Start() {
	cs.logger.Info("starting cron scheduler", "job_count", len(cs.jobs))
	
	cs.wg.Add(1)
	go cs.run()
}

// Stop stops the cron scheduler
func (cs *CronScheduler) Stop() {
	cs.logger.Info("stopping cron scheduler")
	cs.ticker.Stop()
	cs.cancel()
	cs.wg.Wait()
}

// run is the main scheduler loop
func (cs *CronScheduler) run() {
	defer cs.wg.Done()

	for {
		select {
		case <-cs.ctx.Done():
			return
		case now := <-cs.ticker.C:
			cs.checkAndRunJobs(now)
		}
	}
}

// checkAndRunJobs checks all jobs and runs those that are due
func (cs *CronScheduler) checkAndRunJobs(now time.Time) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, job := range cs.jobs {
		job.mu.RLock()
		shouldRun := now.After(job.NextRun) || now.Equal(job.NextRun)
		isRunning := job.State == CronStateRunning
		job.mu.RUnlock()

		if shouldRun && !isRunning {
			cs.wg.Add(1)
			go cs.runJob(job)
		}
	}
}

// runJob executes a single cron job
func (cs *CronScheduler) runJob(job *CronJob) {
	defer cs.wg.Done()

	job.mu.Lock()
	job.State = CronStateRunning
	job.RunCount++
	
	execution := &CronExecution{
		StartTime:    time.Now(),
		AttemptCount: 1,
	}
	job.LastExecution = execution
	
	jobLogger := cs.logger.With(
		"cron_job", job.Config.Name,
		"command", job.Config.Command,
		"run_count", job.RunCount,
	)
	job.mu.Unlock()

	jobLogger.Info("starting cron job execution")

	// Execute with retries
	var lastError error
	for attempt := 1; attempt <= job.Config.Retries+1; attempt++ {
		execution.AttemptCount = attempt
		
		if attempt > 1 {
			jobLogger.Info("retrying cron job", "attempt", attempt, "max_attempts", job.Config.Retries+1)
			time.Sleep(time.Duration(attempt-1) * time.Second) // Simple backoff
		}

		err := cs.executeCommand(job, jobLogger)
		if err == nil {
			// Success
			job.mu.Lock()
			job.State = CronStateIdle
			job.SuccessCount++
			execution.EndTime = &[]time.Time{time.Now()}[0]
			execution.ExitCode = 0
			job.NextRun = cs.calculateNextRun(job.Config.Schedule, time.Now())
			job.mu.Unlock()
			
			jobLogger.Info("cron job completed successfully", 
				"duration", time.Since(execution.StartTime),
				"attempt", attempt)
			return
		}

		lastError = err
		if attempt <= job.Config.Retries {
			jobLogger.Warn("cron job failed, will retry", 
				"attempt", attempt, 
				"error", err)
		}
	}

	// All attempts failed
	job.mu.Lock()
	job.State = CronStateFailed
	job.FailureCount++
	execution.EndTime = &[]time.Time{time.Now()}[0]
	execution.Error = lastError
	if exitError, ok := lastError.(*exec.ExitError); ok {
		execution.ExitCode = exitError.ExitCode()
	} else {
		execution.ExitCode = 1
	}
	job.NextRun = cs.calculateNextRun(job.Config.Schedule, time.Now())
	job.mu.Unlock()

	jobLogger.Error("cron job failed after all retries", 
		"attempts", job.Config.Retries+1,
		"duration", time.Since(execution.StartTime),
		"error", lastError)
}

// executeCommand executes the cron job command
func (cs *CronScheduler) executeCommand(job *CronJob, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), job.Config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", job.Config.Command)

	// Set up logging if enabled
	if job.Config.LogOutput && cs.logManager != nil {
		fileLogger, err := cs.logManager.GetProcessLogger(job.Config.Name)
		if err != nil {
			logger.Warn("failed to get cron job logger", "error", err)
		} else {
			cmd.Stdout = NewMultiWriter(
				fileLogger,
				NewProcessWriter(job.Config.Name, os.Stdout, false),
			)
			cmd.Stderr = NewMultiWriter(
				fileLogger,
				NewProcessWriter(job.Config.Name, os.Stderr, true),
			)
		}
	} else {
		// Just use process writers for stdout/stderr
		cmd.Stdout = NewProcessWriter(job.Config.Name, os.Stdout, false)
		cmd.Stderr = NewProcessWriter(job.Config.Name, os.Stderr, true)
	}

	return cmd.Run()
}

// calculateNextRun calculates the next run time for a given schedule
func (cs *CronScheduler) calculateNextRun(schedule string, from time.Time) time.Time {
	next, err := cs.parseScheduleToNextRun(schedule, from)
	if err != nil {
		// Fallback to 1 hour from now if parsing fails
		cs.logger.Error("failed to parse schedule for next run", "schedule", schedule, "error", err)
		return from.Add(1 * time.Hour)
	}
	return next
}

// GetJob returns a cron job by name
func (cs *CronScheduler) GetJob(name string) *CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.jobs[name]
}

// GetAllJobs returns all cron jobs
func (cs *CronScheduler) GetAllJobs() map[string]*CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	
	jobs := make(map[string]*CronJob)
	for name, job := range cs.jobs {
		jobs[name] = job
	}
	return jobs
}

// GetJobStatus returns the status of a cron job
func (cs *CronScheduler) GetJobStatus(name string) map[string]interface{} {
	job := cs.GetJob(name)
	if job == nil {
		return nil
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	status := map[string]interface{}{
		"name":          job.Config.Name,
		"schedule":      job.Config.Schedule,
		"command":       job.Config.Command,
		"state":         string(job.State),
		"next_run":      job.NextRun.Format(time.RFC3339),
		"run_count":     job.RunCount,
		"success_count": job.SuccessCount,
		"failure_count": job.FailureCount,
	}

	if job.LastExecution != nil {
		execution := map[string]interface{}{
			"start_time":    job.LastExecution.StartTime.Format(time.RFC3339),
			"attempt_count": job.LastExecution.AttemptCount,
			"exit_code":     job.LastExecution.ExitCode,
		}
		
		if job.LastExecution.EndTime != nil {
			execution["end_time"] = job.LastExecution.EndTime.Format(time.RFC3339)
			execution["duration"] = job.LastExecution.EndTime.Sub(job.LastExecution.StartTime).String()
		}
		
		if job.LastExecution.Error != nil {
			execution["error"] = job.LastExecution.Error.Error()
		}
		
		status["last_execution"] = execution
	}

	return status
}

// GetAllJobStatuses returns the status of all cron jobs
func (cs *CronScheduler) GetAllJobStatuses() map[string]interface{} {
	jobs := cs.GetAllJobs()
	statuses := make(map[string]interface{})
	
	for name := range jobs {
		if status := cs.GetJobStatus(name); status != nil {
			statuses[name] = status
		}
	}
	
	return statuses
}