package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestCronHTTPEndpoints tests the HTTP endpoints for cron job monitoring
func TestCronHTTPEndpoints(t *testing.T) {
	// Create test configuration with cron jobs
	config := NewConfig()
	config.HTTPPort = 8080
	
	// Add some test cron jobs
	cronJob1 := config.AddCronJob("test-job-1", "daily", "echo 'test1'")
	cronJob1.Retries = 3
	cronJob1.Timeout = 10 * time.Minute
	
	cronJob2 := config.AddCronJob("test-job-2", "every 5m", "echo 'test2'")
	cronJob2.Retries = 1
	cronJob2.Timeout = 30 * time.Second

	// Create manager
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Create HTTP server
	httpServer := NewHTTPServer(manager, config.HTTPPort)

	// Test /crons endpoint
	t.Run("GET /crons", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/crons", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJobs(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		// Check response structure
		if _, ok := response["cron_jobs"]; !ok {
			t.Error("Response missing 'cron_jobs' field")
		}
		if _, ok := response["count"]; !ok {
			t.Error("Response missing 'count' field")
		}
		if _, ok := response["timestamp"]; !ok {
			t.Error("Response missing 'timestamp' field")
		}

		// Check count
		count, ok := response["count"].(float64)
		if !ok || int(count) != 2 {
			t.Errorf("Expected count 2, got %v", response["count"])
		}

		// Check cron jobs array
		cronJobs, ok := response["cron_jobs"].([]interface{})
		if !ok {
			t.Fatal("cron_jobs is not an array")
		}
		if len(cronJobs) != 2 {
			t.Errorf("Expected 2 cron jobs, got %d", len(cronJobs))
		}

		// Check first cron job details
		job1, ok := cronJobs[0].(map[string]interface{})
		if !ok {
			t.Fatal("First cron job is not an object")
		}

		expectedFields := []string{"name", "schedule", "command", "retries", "timeout", "log_output"}
		for _, field := range expectedFields {
			if _, exists := job1[field]; !exists {
				t.Errorf("Cron job missing field: %s", field)
			}
		}
	})

	// Test /crons/{name} endpoint
	t.Run("GET /crons/test-job-1", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/crons/test-job-1", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJob(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		// Check individual job response
		if response["name"] != "test-job-1" {
			t.Errorf("Expected name 'test-job-1', got %v", response["name"])
		}
		if response["schedule"] != "daily" {
			t.Errorf("Expected schedule 'daily', got %v", response["schedule"])
		}
		if response["command"] != "echo 'test1'" {
			t.Errorf("Expected command 'echo 'test1'', got %v", response["command"])
		}
		if response["retries"] != float64(3) {
			t.Errorf("Expected retries 3, got %v", response["retries"])
		}

		// Check status fields exist
		statusFields := []string{"state", "next_run", "run_count", "success_count", "failure_count"}
		for _, field := range statusFields {
			if _, exists := response[field]; !exists {
				t.Errorf("Response missing status field: %s", field)
			}
		}
	})

	// Test non-existent cron job
	t.Run("GET /crons/non-existent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/crons/non-existent", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJob(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	// Test invalid HTTP methods
	t.Run("POST /crons", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/crons", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJobs(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", w.Code)
		}
	})

	t.Run("PUT /crons/test-job-1", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/crons/test-job-1", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJob(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", w.Code)
		}
	})

	// Test empty path
	t.Run("GET /crons/", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/crons/", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJob(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

// TestCronHTTPEndpointsWithRunningScheduler tests HTTP endpoints with actual running cron scheduler
func TestCronHTTPEndpointsWithRunningScheduler(t *testing.T) {
	// Create logger (unused but needed for manager creation)
	_ = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))

	// Create test configuration
	config := NewConfig()
	config.HTTPPort = 8081
	
	// Add a fast-running test cron job
	cronJob := config.AddCronJob("fast-test", "every 1s", "echo 'fast test'")
	cronJob.Retries = 1
	cronJob.Timeout = 5 * time.Second

	// Create manager
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Start the cron scheduler
	if manager.cronScheduler != nil {
		manager.cronScheduler.Start()
		defer manager.cronScheduler.Stop()
	}

	// Create HTTP server
	httpServer := NewHTTPServer(manager, config.HTTPPort)

	// Wait for at least one execution
	time.Sleep(2 * time.Second)

	// Test that job has run
	t.Run("Job execution status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/crons/fast-test", nil)
		w := httptest.NewRecorder()
		
		httpServer.handleCronJob(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		// Check that job has run
		runCount, ok := response["run_count"]
		if !ok {
			t.Error("Response missing run_count")
		} else if runCountFloat, ok := runCount.(float64); ok {
			if int(runCountFloat) == 0 {
				t.Error("Job should have run at least once")
			}
		}

		// Check state is valid
		state, ok := response["state"]
		if !ok {
			t.Error("Response missing state")
		} else {
			validStates := map[string]bool{"idle": true, "running": true, "failed": true}
			if stateStr, ok := state.(string); ok {
				if !validStates[stateStr] {
					t.Errorf("Invalid state: %s", stateStr)
				}
			}
		}

		// Check next_run is in the future
		nextRun, ok := response["next_run"]
		if !ok {
			t.Error("Response missing next_run")
		} else if nextRunStr, ok := nextRun.(string); ok {
			nextRunTime, err := time.Parse(time.RFC3339, nextRunStr)
			if err != nil {
				t.Errorf("Invalid next_run time format: %s", nextRunStr)
			} else {
				now := time.Now()
				// Allow some tolerance - NextRun might be in the past if job is currently running
				// but hasn't finished yet (NextRun is updated after job completion)
				if nextRunTime.Before(now.Add(-5 * time.Second)) {
					t.Errorf("next_run is too far in the past: next_run=%v, now=%v, diff=%v", 
						nextRunTime, now, nextRunTime.Sub(now))
				}
			}
		}
	})
}

// TestCronHTTPEndpointsIntegration tests the full HTTP integration
func TestCronHTTPEndpointsIntegration(t *testing.T) {
	// Create test server with cron jobs
	config := NewConfig()
	
	// Add multiple test cron jobs with different configurations
	config.AddCronJob("integration-1", "daily", "echo 'integration test 1'")
	config.AddCronJob("integration-2", "hourly", "echo 'integration test 2'")
	config.AddCronJob("", "every 30m", "echo 'auto-named job'") // Should get auto-named

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := manager.Initialize(); err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Create HTTP test server
	httpServer := NewHTTPServer(manager, 0) // Port 0 for test
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crons":
			httpServer.handleCronJobs(w, r)
		default:
			if r.URL.Path[:7] == "/crons/" {
				httpServer.handleCronJob(w, r)
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	defer testServer.Close()

	// Test /crons endpoint
	resp, err := http.Get(testServer.URL + "/crons")
	if err != nil {
		t.Fatalf("Failed to get /crons: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var cronList map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cronList); err != nil {
		t.Fatalf("Failed to decode cron list: %v", err)
	}

	// Should have 3 cron jobs
	if count, ok := cronList["count"].(float64); !ok || int(count) != 3 {
		t.Errorf("Expected 3 cron jobs, got %v", cronList["count"])
	}

	// Test individual job endpoint
	resp, err = http.Get(testServer.URL + "/crons/integration-1")
	if err != nil {
		t.Fatalf("Failed to get /crons/integration-1: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var jobDetail map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&jobDetail); err != nil {
		t.Fatalf("Failed to decode job detail: %v", err)
	}

	// Verify job details
	if jobDetail["name"] != "integration-1" {
		t.Errorf("Expected name 'integration-1', got %v", jobDetail["name"])
	}
	if jobDetail["schedule"] != "daily" {
		t.Errorf("Expected schedule 'daily', got %v", jobDetail["schedule"])
	}

	// Test auto-named job
	resp, err = http.Get(testServer.URL + "/crons/cron-3")
	if err != nil {
		t.Fatalf("Failed to get /crons/cron-3: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for auto-named job, got %d", resp.StatusCode)
	}

	// Test 404 for non-existent job
	resp, err = http.Get(testServer.URL + "/crons/non-existent-job")
	if err != nil {
		t.Fatalf("Failed to get non-existent job: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 for non-existent job, got %d", resp.StatusCode)
	}
}