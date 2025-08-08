package util

import (
	"math"
	"net/http"
	"time"
)

func GetRetryBackoff(attempt int, initBackoff time.Duration, backoffBase float64, maxBackoff time.Duration) time.Duration {
	sleepDuration := initBackoff * time.Duration(math.Pow(backoffBase, float64(attempt)))
	if sleepDuration > maxBackoff {
		sleepDuration = maxBackoff
	}
	return sleepDuration
}

func IsSuccessfulStatusCode(statusCode int) bool {
	return 200 <= statusCode && statusCode < 300
}

func IsRetryableHTTPStatusCodes(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}
