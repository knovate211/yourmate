// Package config reads service settings from the environment.
package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

// String returns the variable or def when it is unset or empty.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// MustString returns the variable or exits when it is missing.
func MustString(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("config: %s is required", key)
	}
	return v
}

// Int returns the variable parsed as an int, or def.
func Int(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("config: %s=%q is not an integer", key, v)
	}
	return n
}

// Bool returns the variable parsed as a bool, or def.
func Bool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("config: %s=%q is not a boolean", key, v)
	}
	return b
}

// Duration returns the variable parsed as a time.Duration, or def.
func Duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("config: %s=%q is not a duration", key, v)
	}
	return d
}
