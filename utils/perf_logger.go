package utils

import (
	"log"
	"time"
)

// PerfLogger tracks performance metrics for batch operations
type PerfLogger struct {
	enabled   bool
	startTime time.Time
	lastLog   time.Time

	totalProcessed int
	lastProcessed  int
}

// NewPerfLogger creates a new performance logger
func NewPerfLogger(enabled bool) *PerfLogger {
	now := time.Now()
	return &PerfLogger{
		enabled:   enabled,
		startTime: now,
		lastLog:   now,
	}
}

// Update increments the processed count and logs if interval has passed
func (p *PerfLogger) Update(processed int, logInterval time.Duration) {
	if !p.enabled {
		return
	}

	p.totalProcessed = processed

	// Log at specified intervals
	now := time.Now()
	if now.Sub(p.lastLog) >= logInterval {
		p.LogCurrent()
		p.lastLog = now
	}
}

// LogCurrent logs current performance metrics
func (p *PerfLogger) LogCurrent() {
	if !p.enabled {
		return
	}

	elapsed := time.Since(p.startTime)
	tps := float64(p.totalProcessed) / elapsed.Seconds()

	log.Printf("[PERF] Processed: %d | Elapsed: %s | TPS: %.2f",
		p.totalProcessed,
		elapsed.Round(time.Second),
		tps)
}

// LogFinal logs final performance summary
func (p *PerfLogger) LogFinal() {
	if !p.enabled {
		return
	}

	elapsed := time.Since(p.startTime)
	tps := float64(p.totalProcessed) / elapsed.Seconds()

	log.Printf("========== PERFORMANCE SUMMARY ==========")
	log.Printf("Total Records:      %d", p.totalProcessed)
	log.Printf("Total Time:         %s", elapsed.Round(time.Millisecond))
	log.Printf("Avg TPS:            %.2f", tps)
	log.Printf("Avg Time/Record:    %s", time.Duration(elapsed.Nanoseconds()/int64(max(p.totalProcessed, 1))).Round(time.Microsecond))
	log.Printf("=========================================")
}
