package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessLogger_WriteToFile(t *testing.T) {
	// Create temporary directory for logs
	tmpDir, err := os.MkdirTemp("", "wardend-test-logs")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logManager := NewLogManager(tmpDir, logger)

	processLogger, err := logManager.GetProcessLogger("test-process")
	if err != nil {
		t.Fatalf("Failed to get process logger: %v", err)
	}

	testData := "This is a test log entry\n"
	n, err := processLogger.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write to process logger: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Close the logger to flush
	if err := processLogger.Close(); err != nil {
		t.Fatalf("Failed to close process logger: %v", err)
	}

	// Verify file was created and contains the data
	logPath := filepath.Join(tmpDir, "test-process.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if string(content) != testData {
		t.Errorf("Expected log content '%s', got '%s'", testData, string(content))
	}
}

func TestProcessLogger_NoLogDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logManager := NewLogManager("", logger)

	processLogger, err := logManager.GetProcessLogger("test-process")
	if err != nil {
		t.Fatalf("Failed to get process logger: %v", err)
	}

	// Should not have a file when no log directory is specified
	if processLogger.currentFile != nil {
		t.Error("Expected no current file when log directory is empty")
	}
}

func TestProcessLogger_Rotation(t *testing.T) {
	// Create temporary directory for logs
	tmpDir, err := os.MkdirTemp("", "wardend-test-logs")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logManager := NewLogManager(tmpDir, logger)

	processLogger, err := logManager.GetProcessLogger("test-process")
	if err != nil {
		t.Fatalf("Failed to get process logger: %v", err)
	}

	// Set small max size to trigger rotation
	processLogger.maxSize = 50
	processLogger.maxBackups = 2

	// Write data that will exceed max size
	testData := strings.Repeat("This is a test log entry that will cause rotation\n", 5)

	_, err = processLogger.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write to process logger: %v", err)
	}

	// Close the logger
	if err := processLogger.Close(); err != nil {
		t.Fatalf("Failed to close process logger: %v", err)
	}

	// Check that backup file was created
	files, err := filepath.Glob(filepath.Join(tmpDir, "test-process.log.*"))
	if err != nil {
		t.Fatalf("Failed to glob backup files: %v", err)
	}

	if len(files) == 0 {
		t.Error("Expected at least one backup file after rotation")
	}
}

func TestMultiWriter(t *testing.T) {
	// Create temporary directory for logs
	tmpDir, err := os.MkdirTemp("", "wardend-test-logs")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a temporary file for testing
	tmpFile, err := os.CreateTemp(tmpDir, "test-output")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = tmpFile.Close() }()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logManager := NewLogManager(tmpDir, logger)

	processLogger, err := logManager.GetProcessLogger("test-process")
	if err != nil {
		t.Fatalf("Failed to get process logger: %v", err)
	}

	// Create multi-writer that writes to both process logger and temp file
	multiWriter := NewMultiWriter(processLogger, tmpFile)

	testData := "This should be written to both outputs\n"
	n, err := multiWriter.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write to multi writer: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Close loggers to flush
	_ = processLogger.Close()
	_ = tmpFile.Sync()

	// Check process log file
	processLogPath := filepath.Join(tmpDir, "test-process.log")
	processContent, err := os.ReadFile(processLogPath)
	if err != nil {
		t.Fatalf("Failed to read process log file: %v", err)
	}

	if string(processContent) != testData {
		t.Errorf("Expected process log content '%s', got '%s'", testData, string(processContent))
	}

	// Check temp file
	_, _ = tmpFile.Seek(0, 0)
	tmpContent := make([]byte, len(testData))
	_, _ = tmpFile.Read(tmpContent)

	if string(tmpContent) != testData {
		t.Errorf("Expected temp file content '%s', got '%s'", testData, string(tmpContent))
	}
}

func TestProcessWriter(t *testing.T) {
	// Create temporary file for testing
	tmpFile, err := os.CreateTemp("", "test-process-writer")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	processWriter := NewProcessWriter("test-process", tmpFile, false)

	testData := "Test log message\n"
	n, err := processWriter.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write to process writer: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Read back the content
	_, _ = tmpFile.Seek(0, 0)
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	contentStr := string(content)

	// Should contain the process name
	if !strings.Contains(contentStr, "test-process") {
		t.Error("Expected output to contain process name")
	}

	// Should contain the original message
	if !strings.Contains(contentStr, "Test log message") {
		t.Error("Expected output to contain original message")
	}

	// Should contain timestamp format
	if !strings.Contains(contentStr, "-") || !strings.Contains(contentStr, ":") {
		t.Error("Expected output to contain timestamp")
	}

	// Should contain log level
	if !strings.Contains(contentStr, "INFO") {
		t.Error("Expected output to contain log level INFO")
	}
}

func TestProcessWriter_Stderr(t *testing.T) {
	// Create temporary file for testing
	tmpFile, err := os.CreateTemp("", "test-process-writer")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	processWriter := NewProcessWriter("test-process", tmpFile, true)

	testData := "Error message\n"
	_, err = processWriter.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write to process writer: %v", err)
	}

	// Read back the content
	_, _ = tmpFile.Seek(0, 0)
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	contentStr := string(content)

	// Should contain ERROR level for stderr
	if !strings.Contains(contentStr, "ERROR") {
		t.Error("Expected stderr output to contain log level ERROR")
	}
}

func TestLogManager_MultipleProcesses(t *testing.T) {
	// Create temporary directory for logs
	tmpDir, err := os.MkdirTemp("", "wardend-test-logs")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logManager := NewLogManager(tmpDir, logger)
	defer func() { _ = logManager.CloseAll() }()

	// Create loggers for multiple processes
	logger1, err := logManager.GetProcessLogger("process1")
	if err != nil {
		t.Fatalf("Failed to get logger1: %v", err)
	}

	logger2, err := logManager.GetProcessLogger("process2")
	if err != nil {
		t.Fatalf("Failed to get logger2: %v", err)
	}

	// Write different data to each logger
	_, _ = logger1.Write([]byte("Process 1 log\n"))
	_, _ = logger2.Write([]byte("Process 2 log\n"))

	// Close loggers to flush
	_ = logger1.Close()
	_ = logger2.Close()

	// Verify separate log files were created
	log1Path := filepath.Join(tmpDir, "process1.log")
	log2Path := filepath.Join(tmpDir, "process2.log")

	content1, err := os.ReadFile(log1Path)
	if err != nil {
		t.Fatalf("Failed to read process1 log: %v", err)
	}

	content2, err := os.ReadFile(log2Path)
	if err != nil {
		t.Fatalf("Failed to read process2 log: %v", err)
	}

	if string(content1) != "Process 1 log\n" {
		t.Errorf("Expected process1 log 'Process 1 log\\n', got '%s'", string(content1))
	}

	if string(content2) != "Process 2 log\n" {
		t.Errorf("Expected process2 log 'Process 2 log\\n', got '%s'", string(content2))
	}
}
