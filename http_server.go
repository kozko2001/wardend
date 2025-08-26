package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type HTTPServer struct {
	server  *http.Server
	manager *Manager
	logger  *slog.Logger
	mu      sync.RWMutex
}

type HealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Summary   map[string]interface{} `json:"summary"`
}

type StatusResponse struct {
	Status    string                       `json:"status"`
	Timestamp string                       `json:"timestamp"`
	Processes map[string]ProcessStatusInfo `json:"processes"`
}

type ProcessStatusInfo struct {
	State           string   `json:"state"`
	LastStart       string   `json:"last_start,omitempty"`
	StartupAttempts int      `json:"startup_attempts"`
	RuntimeRestarts int      `json:"runtime_restarts"`
	StartupComplete bool     `json:"startup_complete"`
	Command         string   `json:"command"`
	RestartPolicy   string   `json:"restart_policy"`
	DependsOn       []string `json:"depends_on,omitempty"`
	HealthStatus    string   `json:"health_status,omitempty"`
}

func NewHTTPServer(manager *Manager, port int) *HTTPServer {
	mux := http.NewServeMux()

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	httpServer := &HTTPServer{
		server:  server,
		manager: manager,
		logger:  manager.logger.With("component", "http_server"),
	}

	// Register routes
	mux.HandleFunc("/health", httpServer.handleHealth)
	mux.HandleFunc("/status", httpServer.handleStatus)
	mux.HandleFunc("/processes", httpServer.handleProcesses)
	mux.HandleFunc("/processes/", httpServer.handleProcess)
	mux.HandleFunc("/crons", httpServer.handleCronJobs)
	mux.HandleFunc("/crons/", httpServer.handleCronJob)

	return httpServer
}

func (hs *HTTPServer) Start() error {
	hs.logger.Info("starting HTTP health check server", "addr", hs.server.Addr)

	go func() {
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			hs.logger.Error("HTTP server failed", "error", err)
		}
	}()

	return nil
}

func (hs *HTTPServer) Stop() error {
	hs.logger.Info("stopping HTTP health check server")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return hs.server.Shutdown(ctx)
}

func (hs *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	summary := hs.manager.GetHealthSummary()

	status := "healthy"
	statusCode := http.StatusOK

	switch summary["overall_status"] {
	case "unhealthy":
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	case "unknown":
		status = "unknown"
		statusCode = http.StatusServiceUnavailable
	}

	response := HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary:   summary,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode health response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("health check request served",
		"status", status,
		"status_code", statusCode,
		"client_ip", r.RemoteAddr)
}

func (hs *HTTPServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hs.mu.RLock()
	defer hs.mu.RUnlock()

	allStates := hs.manager.GetAllProcessStates()
	allHealthStatuses := hs.manager.GetAllHealthStatuses()

	processes := make(map[string]ProcessStatusInfo)
	overallHealthy := true

	for _, processConfig := range hs.manager.config.Processes {
		name := processConfig.Name
		state := allStates[name]

		info := ProcessStatusInfo{
			State:           string(state),
			StartupAttempts: 0,
			RuntimeRestarts: 0,
			StartupComplete: false,
			Command:         processConfig.Command,
			RestartPolicy:   string(processConfig.RestartPolicy),
			DependsOn:       processConfig.DependsOn,
		}

		// Get process details
		if process, exists := hs.manager.processes[name]; exists {
			process.mu.RLock()
			info.StartupAttempts = process.StartupAttempts
			info.RuntimeRestarts = process.RuntimeRestarts
			info.StartupComplete = process.StartupComplete
			if !process.LastStart.IsZero() {
				info.LastStart = process.LastStart.UTC().Format(time.RFC3339)
			}
			process.mu.RUnlock()
		}

		// Get health status
		if healthResult, exists := allHealthStatuses[name]; exists {
			info.HealthStatus = string(healthResult.Status)
			if healthResult.Status != HealthStatusHealthy {
				overallHealthy = false
			}
		}

		if state != StateRunning {
			overallHealthy = false
		}

		processes[name] = info
	}

	status := "healthy"
	if !overallHealthy {
		status = "degraded"
	}

	response := StatusResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Processes: processes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode status response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("status request served",
		"status", status,
		"process_count", len(processes),
		"client_ip", r.RemoteAddr)
}

func (hs *HTTPServer) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hs.mu.RLock()
	defer hs.mu.RUnlock()

	processes := make([]map[string]interface{}, 0, len(hs.manager.config.Processes))

	for _, processConfig := range hs.manager.config.Processes {
		processInfo := map[string]interface{}{
			"name":            processConfig.Name,
			"command":         processConfig.Command,
			"restart_policy":  string(processConfig.RestartPolicy),
			"depends_on":      processConfig.DependsOn,
			"health_check":    processConfig.HealthCheck,
			"health_interval": processConfig.HealthInterval.String(),
		}

		processes = append(processes, processInfo)
	}

	response := map[string]interface{}{
		"processes": processes,
		"count":     len(processes),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode processes response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("processes list request served",
		"process_count", len(processes),
		"client_ip", r.RemoteAddr)
}

func (hs *HTTPServer) handleProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract process name from path
	path := r.URL.Path
	if len(path) <= len("/processes/") {
		http.Error(w, "Process name required", http.StatusBadRequest)
		return
	}

	processName := path[len("/processes/"):]

	hs.mu.RLock()
	defer hs.mu.RUnlock()

	// Find process config
	var processConfig *ProcessConfig
	for _, config := range hs.manager.config.Processes {
		if config.Name == processName {
			processConfig = &config
			break
		}
	}

	if processConfig == nil {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	// Get current state
	state, _ := hs.manager.GetProcessState(processName)
	healthStatus := hs.manager.GetHealthStatus(processName)

	response := map[string]interface{}{
		"name":            processConfig.Name,
		"command":         processConfig.Command,
		"restart_policy":  string(processConfig.RestartPolicy),
		"depends_on":      processConfig.DependsOn,
		"health_check":    processConfig.HealthCheck,
		"health_interval": processConfig.HealthInterval.String(),
		"state":           string(state),
		"health_status":   string(healthStatus.Status),
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	}

	// Add process runtime details if available
	if process, exists := hs.manager.processes[processName]; exists {
		process.mu.RLock()
		response["startup_attempts"] = process.StartupAttempts
		response["runtime_restarts"] = process.RuntimeRestarts
		response["startup_complete"] = process.StartupComplete
		if !process.LastStart.IsZero() {
			response["last_start"] = process.LastStart.UTC().Format(time.RFC3339)
		}
		process.mu.RUnlock()
	}

	// Add health check details if available
	if healthStatus.Status != HealthStatusUnknown {
		response["health_last_check"] = healthStatus.Timestamp.UTC().Format(time.RFC3339)
		response["health_duration"] = healthStatus.Duration.String()
		if healthStatus.Error != nil {
			response["health_error"] = healthStatus.Error.Error()
		}
		if healthStatus.Output != "" {
			response["health_output"] = healthStatus.Output
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode process response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("process details request served",
		"process", processName,
		"state", state,
		"client_ip", r.RemoteAddr)
}

func (hs *HTTPServer) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hs.mu.RLock()
	defer hs.mu.RUnlock()

	cronJobs := make([]map[string]interface{}, 0, len(hs.manager.config.CronJobs))

	for _, cronConfig := range hs.manager.config.CronJobs {
		cronInfo := map[string]interface{}{
			"name":       cronConfig.Name,
			"schedule":   cronConfig.Schedule,
			"command":    cronConfig.Command,
			"retries":    cronConfig.Retries,
			"timeout":    cronConfig.Timeout.String(),
			"log_output": cronConfig.LogOutput,
		}

		// Add runtime status if available
		if status := hs.manager.GetCronJobStatus(cronConfig.Name); status != nil {
			cronInfo["state"] = status["state"]
			cronInfo["next_run"] = status["next_run"]
			cronInfo["run_count"] = status["run_count"]
			cronInfo["success_count"] = status["success_count"]
			cronInfo["failure_count"] = status["failure_count"]
			if lastExecution, ok := status["last_execution"]; ok {
				cronInfo["last_execution"] = lastExecution
			}
		}

		cronJobs = append(cronJobs, cronInfo)
	}

	response := map[string]interface{}{
		"cron_jobs": cronJobs,
		"count":     len(cronJobs),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode cron jobs response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("cron jobs list request served",
		"cron_job_count", len(cronJobs),
		"client_ip", r.RemoteAddr)
}

func (hs *HTTPServer) handleCronJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract cron job name from path
	path := r.URL.Path
	if len(path) <= len("/crons/") {
		http.Error(w, "Cron job name required", http.StatusBadRequest)
		return
	}

	cronJobName := path[len("/crons/"):]

	hs.mu.RLock()
	defer hs.mu.RUnlock()

	// Find cron job config
	var cronConfig *CronConfig
	for _, config := range hs.manager.config.CronJobs {
		if config.Name == cronJobName {
			cronConfig = &config
			break
		}
	}

	if cronConfig == nil {
		http.Error(w, "Cron job not found", http.StatusNotFound)
		return
	}

	// Get current status
	status := hs.manager.GetCronJobStatus(cronJobName)
	if status == nil {
		http.Error(w, "Cron job status not available", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"name":       cronConfig.Name,
		"schedule":   cronConfig.Schedule,
		"command":    cronConfig.Command,
		"retries":    cronConfig.Retries,
		"timeout":    cronConfig.Timeout.String(),
		"log_output": cronConfig.LogOutput,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	// Add all status information
	for key, value := range status {
		response[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		hs.logger.Error("failed to encode cron job response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hs.logger.Debug("cron job details request served",
		"cron_job", cronJobName,
		"state", status["state"],
		"client_ip", r.RemoteAddr)
}
