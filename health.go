package main

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

type HealthCheckResult struct {
	Status    HealthStatus
	Output    string
	Error     error
	Timestamp time.Time
	Duration  time.Duration
}

type HealthChecker struct {
	manager *Manager
	results map[string]*HealthCheckResult
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewHealthChecker(manager *Manager) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	return &HealthChecker{
		manager: manager,
		results: make(map[string]*HealthCheckResult),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (h *HealthChecker) Start() {
	h.manager.logger.Info("starting health checker")

	// Start health checks for each process that has them configured
	for _, processConfig := range h.manager.config.Processes {
		if processConfig.HealthCheck != "" {
			h.wg.Add(1)
			go h.runHealthChecks(processConfig.Name, processConfig.HealthCheck, processConfig.HealthInterval)
		}
	}
}

func (h *HealthChecker) Stop() {
	h.manager.logger.Info("stopping health checker")
	h.cancel()
	h.wg.Wait()
}

func (h *HealthChecker) runHealthChecks(processName, healthCommand string, interval time.Duration) {
	defer h.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial health check immediately
	h.executeHealthCheck(processName, healthCommand)

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			// Only run health check if process is running
			state, err := h.manager.GetProcessState(processName)
			if err != nil {
				h.manager.logger.Debug("skipping health check for unknown process", "name", processName, "error", err)
				continue
			}

			if state != StateRunning {
				h.manager.logger.Debug("skipping health check for non-running process", "name", processName, "state", state)
				continue
			}

			h.executeHealthCheck(processName, healthCommand)
		}
	}
}

func (h *HealthChecker) executeHealthCheck(processName, healthCommand string) {
	start := time.Now()
	result := &HealthCheckResult{
		Timestamp: start,
		Status:    HealthStatusUnknown,
	}

	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second) // 30 second timeout for health checks
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", healthCommand)
	output, err := cmd.CombinedOutput()

	result.Duration = time.Since(start)
	result.Output = string(output)
	result.Error = err

	if err != nil {
		result.Status = HealthStatusUnhealthy
		h.manager.logger.Warn("health check failed",
			"process", processName,
			"command", healthCommand,
			"duration", result.Duration,
			"output", string(output),
			"error", err)
	} else {
		result.Status = HealthStatusHealthy
		h.manager.logger.Debug("health check passed",
			"process", processName,
			"command", healthCommand,
			"duration", result.Duration)
	}

	h.mu.Lock()
	h.results[processName] = result
	h.mu.Unlock()
}

func (h *HealthChecker) GetHealthStatus(processName string) *HealthCheckResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result, exists := h.results[processName]
	if !exists {
		return &HealthCheckResult{
			Status:    HealthStatusUnknown,
			Timestamp: time.Now(),
		}
	}

	// Create a copy to avoid data races
	return &HealthCheckResult{
		Status:    result.Status,
		Output:    result.Output,
		Error:     result.Error,
		Timestamp: result.Timestamp,
		Duration:  result.Duration,
	}
}

func (h *HealthChecker) GetAllHealthStatuses() map[string]*HealthCheckResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	results := make(map[string]*HealthCheckResult)
	for name, result := range h.results {
		// Create a copy to avoid data races
		results[name] = &HealthCheckResult{
			Status:    result.Status,
			Output:    result.Output,
			Error:     result.Error,
			Timestamp: result.Timestamp,
			Duration:  result.Duration,
		}
	}

	return results
}

// IsOverallHealthy returns true if all processes with health checks are healthy
func (h *HealthChecker) IsOverallHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, result := range h.results {
		if result.Status != HealthStatusHealthy {
			return false
		}
	}

	return true
}

// GetHealthSummary returns a summary of all health check results
func (h *HealthChecker) GetHealthSummary() map[string]interface{} {
	allResults := h.GetAllHealthStatuses()

	summary := map[string]interface{}{
		"overall_status":  "healthy",
		"total_checks":    len(allResults),
		"healthy_count":   0,
		"unhealthy_count": 0,
		"unknown_count":   0,
		"checks":          make(map[string]interface{}),
	}

	healthyCount := 0
	unhealthyCount := 0
	unknownCount := 0

	checks := make(map[string]interface{})
	for name, result := range allResults {
		switch result.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		case HealthStatusUnknown:
			unknownCount++
		}

		checks[name] = map[string]interface{}{
			"status":    string(result.Status),
			"timestamp": result.Timestamp.Format(time.RFC3339),
			"duration":  result.Duration.String(),
		}

		if result.Error != nil {
			checks[name].(map[string]interface{})["error"] = result.Error.Error()
		}
		if result.Output != "" {
			checks[name].(map[string]interface{})["output"] = result.Output
		}
	}

	summary["healthy_count"] = healthyCount
	summary["unhealthy_count"] = unhealthyCount
	summary["unknown_count"] = unknownCount
	summary["checks"] = checks

	if unhealthyCount > 0 {
		summary["overall_status"] = "unhealthy"
	} else if unknownCount > 0 && healthyCount == 0 {
		summary["overall_status"] = "unknown"
	}

	return summary
}
