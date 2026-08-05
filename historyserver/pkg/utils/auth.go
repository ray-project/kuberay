package utils

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	RayTokenMountPath                 = "/var/run/secrets/ray.io/serviceaccount"
	RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR = "RAY_ENABLE_K8S_TOKEN_AUTH"
	RAY_AUTH_TOKEN_ENV_VAR            = "RAY_AUTH_TOKEN"
	RAY_AUTH_MODE_ENV_VAR             = "RAY_AUTH_MODE"

	RAY_AUTH_TOKEN_SECRET_KEY = "auth_token"
	RayAuthModeToken          = "token"
	RayAuthHeader             = "x-ray-authorization"
	RayK8sTokenPath           = RayTokenMountPath + "/token"
)

func IsRayAuthEnabled() bool {
	return strings.EqualFold(os.Getenv(RAY_AUTH_MODE_ENV_VAR), RayAuthModeToken)
}

func isK8sTokenAuthEnabled() bool {
	enabled, err := strconv.ParseBool(os.Getenv(RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR))
	return err == nil && enabled
}

func staticAuthToken() string {
	return strings.TrimSpace(os.Getenv(RAY_AUTH_TOKEN_ENV_VAR))
}

func getRayAuthToken() (string, error) {
	switch {
	case isK8sTokenAuthEnabled():
		raw, err := os.ReadFile(RayK8sTokenPath)
		if err != nil {
			return "", fmt.Errorf("failed to read projected ServiceAccount token at %s: %w", RayK8sTokenPath, err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("projected ServiceAccount token at %s is empty", RayK8sTokenPath)
		}
		return token, nil
	case staticAuthToken() != "":
		return staticAuthToken(), nil
	case IsRayAuthEnabled():
		return "", fmt.Errorf("%s=%s but no credential is available: set %s, or %s=true with the projected token volume mounted at %s",
			RAY_AUTH_MODE_ENV_VAR, RayAuthModeToken, RAY_AUTH_TOKEN_ENV_VAR, RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR, RayTokenMountPath)
	default:
		return "", nil
	}
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
