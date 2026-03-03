package timings

import (
	"log"
	"os"
	"strings"
	"sync"
)

var (
	enabled         bool
	detailedEnabled bool
	once            sync.Once
	detailedOnce    sync.Once
)

// Enabled returns true when WHISKER_TIMINGS is set to a truthy value.
// Truthy: any non-empty value except "0" and "false" (case-insensitive).
func Enabled() bool {
	once.Do(func() {
		v := strings.TrimSpace(os.Getenv("WHISKER_TIMINGS"))
		if v == "" {
			enabled = false
			return
		}
		switch strings.ToLower(v) {
		case "0", "false":
			enabled = false
		default:
			enabled = true
		}
	})
	return enabled
}

// DetailedEnabled returns true when DETAILED_TRANSCRIPTION_LOGGING is set to a truthy value.
// Truthy: any non-empty value except "0" and "false" (case-insensitive).
func DetailedEnabled() bool {
	detailedOnce.Do(func() {
		v := strings.TrimSpace(os.Getenv("DETAILED_TRANSCRIPTION_LOGGING"))
		if v == "" {
			detailedEnabled = false
			return
		}
		switch strings.ToLower(v) {
		case "0", "false":
			detailedEnabled = false
		default:
			detailedEnabled = true
		}
	})
	return detailedEnabled
}

// Printf emits a log line only when timings are enabled.
func Printf(format string, args ...any) {
	if !Enabled() {
		return
	}
	log.Printf(format, args...)
}

// Detailedf emits a log line only when detailed logging is enabled.
func Detailedf(format string, args ...any) {
	if !DetailedEnabled() {
		return
	}
	log.Printf(format, args...)
}
