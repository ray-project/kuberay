package utils

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func IsRayAuthEnabledFromEnv() bool {
	return strings.EqualFold(os.Getenv(RAY_AUTH_MODE_ENV_VAR), RayAuthModeToken) ||
		isK8sTokenAuthEnabled() ||
		os.Getenv(RAY_AUTH_TOKEN_ENV_VAR) != ""
}

func isK8sTokenAuthEnabled() bool {
	return strings.EqualFold(os.Getenv(RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR), "true")
}

func getRayAuthToken() (string, error) {
	if !IsRayAuthEnabledFromEnv() {
		return "", nil
	}

	if isK8sTokenAuthEnabled() {
		token, err := os.ReadFile(RayK8sTokenPath)
		if err != nil {
			return "", fmt.Errorf("failed to read projected ServiceAccount token at %s: %w", RayK8sTokenPath, err)
		}
		if len(token) == 0 {
			return "", fmt.Errorf("projected ServiceAccount token at %s is empty", RayK8sTokenPath)
		}
		return string(token), nil
	}

	// Auth is enabled but the Secret behind RAY_AUTH_TOKEN carries no value.
	token := os.Getenv(RAY_AUTH_TOKEN_ENV_VAR)
	if token == "" {
		return "", fmt.Errorf("%s is %q but %s is empty: check the auth token Secret referenced by the collector container",
			RAY_AUTH_MODE_ENV_VAR, os.Getenv(RAY_AUTH_MODE_ENV_VAR), RAY_AUTH_TOKEN_ENV_VAR)
	}
	return token, nil
}

func SetRayAuthHeader(req *http.Request) error {
	token, err := getRayAuthToken()
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set(RayAuthHeader, "Bearer "+token)
	}
	return nil
}

func IsAuthFailure(statusCode int) bool {
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}
