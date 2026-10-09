// Package envconfig reads twelve-factor style configuration from environment variables.
package envconfig

import (
	"fmt"
	"os"
	"time"
)

// String returns the value of key, or def when it is unset or empty.
func String(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Duration parses key as a time.Duration, returning def when it is unset or empty.
func Duration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("envconfig: %s: %w", key, err)
	}
	return d, nil
}
