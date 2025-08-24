package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ProcessLogger struct {
	name       string
	logDir     string
	maxSize    int64         // Maximum size in bytes before rotation
	maxAge     time.Duration // Maximum age before rotation
	maxBackups int           // Maximum number of backup files to keep

	currentFile *os.File
	currentSize int64
	mu          sync.Mutex
}

type LogManager struct {
	processLoggers map[string]*ProcessLogger
	logDir         string
	logger         *slog.Logger
	mu             sync.RWMutex
}

func NewLogManager(logDir string, logger *slog.Logger) *LogManager {
	return &LogManager{
		processLoggers: make(map[string]*ProcessLogger),
		logDir:         logDir,
		logger:         logger,
	}
}

func (lm *LogManager) GetProcessLogger(processName string) (*ProcessLogger, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if logger, exists := lm.processLoggers[processName]; exists {
		return logger, nil
	}

	// Create new process logger
	logger := &ProcessLogger{
		name:       processName,
		logDir:     lm.logDir,
		maxSize:    100 * 1024 * 1024, // 100MB default
		maxAge:     24 * time.Hour,    // 24 hours default
		maxBackups: 5,                 // 5 backups default
	}

	if err := logger.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize logger for process %s: %v", processName, err)
	}

	lm.processLoggers[processName] = logger
	return logger, nil
}

func (lm *LogManager) CloseAll() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	var errors []error
	for _, logger := range lm.processLoggers {
		if err := logger.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors closing process loggers: %v", errors)
	}

	return nil
}

func (pl *ProcessLogger) init() error {
	if pl.logDir == "" {
		// If no log directory specified, log to stdout
		return nil
	}

	// Ensure log directory exists
	if err := os.MkdirAll(pl.logDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Open current log file
	logPath := filepath.Join(pl.logDir, fmt.Sprintf("%s.log", pl.name))
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	// Get current file size
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close() // Ignore close error since we're already returning an error
		return fmt.Errorf("failed to stat log file: %v", err)
	}

	pl.currentFile = file
	pl.currentSize = stat.Size()

	return nil
}

func (pl *ProcessLogger) Write(p []byte) (n int, err error) {
	if pl.logDir == "" {
		// Write to stdout if no log directory
		return os.Stdout.Write(p)
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Check if we need to rotate
	if pl.currentSize+int64(len(p)) > pl.maxSize {
		if err := pl.rotate(); err != nil {
			return 0, fmt.Errorf("failed to rotate log: %v", err)
		}
	}

	// Write to current file
	n, err = pl.currentFile.Write(p)
	if err != nil {
		return n, err
	}

	pl.currentSize += int64(n)
	return n, nil
}

func (pl *ProcessLogger) rotate() error {
	if pl.currentFile == nil {
		return nil
	}

	// Close current file
	if err := pl.currentFile.Close(); err != nil {
		return err
	}

	// Get timestamp for backup
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	currentPath := filepath.Join(pl.logDir, fmt.Sprintf("%s.log", pl.name))
	backupPath := filepath.Join(pl.logDir, fmt.Sprintf("%s.log.%s", pl.name, timestamp))

	// Rename current file to backup
	if err := os.Rename(currentPath, backupPath); err != nil {
		return err
	}

	// Clean up old backups
	if err := pl.cleanupOldBackups(); err != nil {
		// Log warning but don't fail rotation
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup old backups: %v\n", err)
	}

	// Create new current file
	file, err := os.OpenFile(currentPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	pl.currentFile = file
	pl.currentSize = 0

	return nil
}

func (pl *ProcessLogger) cleanupOldBackups() error {
	pattern := filepath.Join(pl.logDir, fmt.Sprintf("%s.log.*", pl.name))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	if len(matches) <= pl.maxBackups {
		return nil
	}

	// Sort by modification time and remove oldest
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var files []fileInfo
	for _, match := range matches {
		stat, err := os.Stat(match)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: match, modTime: stat.ModTime()})
	}

	// Sort by modification time (oldest first)
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].modTime.After(files[j].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	// Remove oldest files beyond maxBackups
	for i := 0; i < len(files)-pl.maxBackups; i++ {
		if err := os.Remove(files[i].path); err != nil {
			return err
		}
	}

	return nil
}

func (pl *ProcessLogger) Close() error {
	if pl.currentFile == nil {
		return nil
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()

	return pl.currentFile.Close()
}

// MultiWriter creates an io.Writer that writes to both the process logger and stdout/stderr
type MultiWriter struct {
	writers []io.Writer
}

func NewMultiWriter(writers ...io.Writer) *MultiWriter {
	return &MultiWriter{writers: writers}
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, w := range mw.writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
	}
	return len(p), nil
}

// ProcessWriter wraps writes with process name prefix for structured logging
type ProcessWriter struct {
	processName string
	underlying  io.Writer
	isStderr    bool
}

func NewProcessWriter(processName string, underlying io.Writer, isStderr bool) *ProcessWriter {
	return &ProcessWriter{
		processName: processName,
		underlying:  underlying,
		isStderr:    isStderr,
	}
}

func (pw *ProcessWriter) Write(p []byte) (n int, err error) {
	// Add timestamp and process name prefix
	timestamp := time.Now().Format("2006-01-02T15:04:05.000Z")
	level := "INFO"
	if pw.isStderr {
		level = "ERROR"
	}

	prefix := fmt.Sprintf("[%s] [%s] [%s] ", timestamp, pw.processName, level)

	// Write prefix + original content
	prefixed := append([]byte(prefix), p...)
	_, err = pw.underlying.Write(prefixed)
	if err != nil {
		return 0, err
	}

	// Return the length of the original data, not the prefixed data
	return len(p), nil
}
