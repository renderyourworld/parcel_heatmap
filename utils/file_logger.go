package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sets up logging to both stdout and a file in the logs directory
func SetupFileLogger(county, operation string) (cleanup func(), err error) {
	// Create logs directory if it doesn't exist
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Clean strings and build filename as county_operation_date.log
	timestamp := time.Now().Format("2006-01-02_150405")
	safeCounty := strings.ToLower(strings.ReplaceAll(county, " ", "_"))
	safeOperation := strings.ToLower(strings.ReplaceAll(operation, " ", "_"))
	filename := fmt.Sprintf("%s_%s_%s.log", safeCounty, safeOperation, timestamp)
	filepath := filepath.Join(logsDir, filename)

	// Open log file
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create multi-writer for both stdout and file
	multiWriter := io.MultiWriter(os.Stdout, file)

	// Set log output to multi-writer
	log.SetOutput(multiWriter)

	log.Printf("Logging to file: %s", filepath)

	// Return cleanup function
	cleanup = func() {
		log.SetOutput(os.Stdout) // Restore default
		file.Close()
	}

	return cleanup, nil
}
