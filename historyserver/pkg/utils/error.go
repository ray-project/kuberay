package utils

// StatusClientClosedRequest is a non-standard HTTP status code used by nginx
// to indicate that the client closed the connection before the server responded.
const StatusClientClosedRequest = 499

// HTTPError represents an error with an associated HTTP status code.
type HTTPError struct {
	internalError error  // Original error
	message       string // Error message from internalError
	statusCode    int    // HTTP status code
}

// NewHTTPError creates a new HTTPError with the given error and status code.
func NewHTTPError(err error, statusCode int) *HTTPError {
	return &HTTPError{
		internalError: err,
		message:       err.Error(),
		statusCode:    statusCode,
	}
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return e.message
}

// Unwrap returns the wrapped error for use with errors.Is and errors.As.
func (e *HTTPError) Unwrap() error {
	return e.internalError
}

// StatusCode returns the status code in HTTPError.
func (e *HTTPError) StatusCode() int {
	return e.statusCode
}
