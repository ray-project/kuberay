package util

import "time"

const (
	// Max retry times for HTTP Client
	HTTPClientDefaultMaxRetry = 3

	// Retry backoff settings
	HTTPClientDefaultBackoffBase = float64(2)
	HTTPClientDefaultInitBackoff = 500 * time.Millisecond
	HTTPClientDefaultMaxBackoff  = 10 * time.Second

	// Overall timeout for retries
	HTTPClientDefaultOverallTimeout = 30 * time.Second
)
