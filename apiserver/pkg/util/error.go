package util

import (
	"fmt"

	klog "k8s.io/klog/v2"

	"github.com/go-openapi/runtime"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	k8metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CustomCode uint32

const (
	CUSTOM_CODE_TRANSIENT CustomCode = 0
	CUSTOM_CODE_PERMANENT CustomCode = 1
	CUSTOM_CODE_NOT_FOUND CustomCode = 2
	CUSTOM_CODE_GENERIC   CustomCode = 3
)

type APICode int

const (
	API_CODE_NOT_FOUND = 404
)

type CustomError struct {
	error error
	code  CustomCode
}

func NewCustomError(err error, code CustomCode, format string, a ...interface{}) *CustomError {
	message := fmt.Sprintf(format, a...)
	return &CustomError{
		error: errors.Wrapf(err, "CustomError (code: %v): %v", code, message),
		code:  code,
	}
}

func NewCustomErrorf(code CustomCode, format string, a ...interface{}) *CustomError {
	message := fmt.Sprintf(format, a...)
	return &CustomError{
		error: errors.Errorf("CustomError (code: %v): %v", code, message),
		code:  code,
	}
}

func (e *CustomError) Error() string {
	return e.error.Error()
}

func HasCustomCode(err error, code CustomCode) bool {
	if err == nil {
		return false
	}

	var customErr *CustomError
	if errors.As(err, &customErr) {
		return customErr.code == code
	}

	return false
}

type UserError struct {
	// Error for internal debugging.
	internalError error
	// Error message for the external client.
	externalMessage string
	// Status code for the external client.
	externalStatusCode codes.Code
}

func newUserError(internalError error, externalMessage string,
	externalStatusCode codes.Code,
) *UserError {
	return &UserError{
		internalError:      internalError,
		externalMessage:    externalMessage,
		externalStatusCode: externalStatusCode,
	}
}

func NewUserErrorWithSingleMessage(err error, message string) *UserError {
	return NewUserError(err, message, message)
}

// Util function to convert HTTP status code to gRPC status code.
func convertHttpStatusToUserError(httpStatusCode int) codes.Code {
	switch httpStatusCode {
	// Success and redirect responses
	case 200, // OK
		201, // Created
		204, // No Content
		206, // Partial Content
		302, // Found
		307, // Temporary Redirect
		308: // Permanent Redirect
		return codes.OK

	// OUT_OF_RANGE indicates requested range is out of scope.
	case 416: // Requested Range Not Satisfiable
		return codes.OutOfRange

	// INVALID_ARGUMENT indicates a problem with how the request is constructed.
	case 400, // Bad Request
		411, // Length Required
		413: // Payload Too Large
		return codes.InvalidArgument

	// UNAUTHENTICATED indicates an authentication issue.
	case 401: // Unauthorized
		return codes.Unauthenticated

	// PERMISSION_DENIED indicates an authorization issue.
	case 403: // Forbidden
		return codes.PermissionDenied

	// NOT_FOUND indicates that the requested resource does not exist.
	case 404, // Not Found
		410: // Gone
		return codes.NotFound

	// FAILED_PRECONDITION indicates that the request failed because some
	// of the underlying assumptions were not satisfied. The request
	// shouldn't be retried unless the external context has changed.
	case 303, // See Other
		304, // Not Modified
		409, // Conflict
		412: // Precondition Failed
		return codes.FailedPrecondition

	// UNAVAILABLE indicates a problem that can go away if the request
	// is just retried without any modification. 308 return codes are intended
	// for write requests that can be retried. See the documentation and the
	// official library:
	// https://cloud.google.com/storage/docs/json_api/v1/how-tos/resumable-upload
	// https://github.com/google/apitools/blob/master/apitools/base/py/transfer.py
	case 429, // Too Many Requests
		503, // Service Unavailable
		504: // Gateway Timeout
		return codes.Unavailable

	// INTERNAL indicates server encountered an unexpected condition that
	// prevented it from fulfilling the request.
	case 500: // Internal Server Error
		return codes.Internal

	// Unknown or all other errors
	case 502: // Bad Gateway
	default:
		return codes.Unknown
	}

	return codes.Unknown
}

func NewUserError(err error, internalMessage string, externalMessage string) *UserError {
	// Note apiError.Response is of type github.com/go-openapi/runtime/client
	var apiError *runtime.APIError
	if errors.As(err, &apiError) {
		if apiError.Code == API_CODE_NOT_FOUND {
			return newUserError(
				errors.Wrapf(err, internalMessage),
				fmt.Sprintf("%v: %v", externalMessage, "Resource not found"),
				convertHttpStatusToUserError(apiError.Code))
		}
	}

	return newUserError(
		errors.Wrapf(err, internalMessage),
		fmt.Sprintf("%v. Raw error from the service: %v", externalMessage, err.Error()),
		codes.Internal)
}

func ExtractErrorForCLI(err error, isDebugMode bool) error {
	// Check if the error is of type *UserError, even if it's wrapped
	var userError *UserError
	if errors.As(err, &userError) {
		if isDebugMode {
			return fmt.Errorf("%+w", userError.internalError)
		}
		return fmt.Errorf("%v", userError.externalMessage)
	}
	return err
}

func NewInternalServerError(err error, internalMessageFormat string,
	a ...interface{},
) *UserError {
	internalMessage := fmt.Sprintf(internalMessageFormat, a...)
	return newUserError(
		errors.Wrapf(err, "InternalServerError: %v", internalMessage),
		"Internal Server Error",
		codes.Internal)
}

func NewNotFoundError(err error, externalMessageFormat string,
	a ...interface{},
) *UserError {
	externalMessage := fmt.Sprintf(externalMessageFormat, a...)
	return newUserError(
		errors.Wrapf(err, "NotFoundError: %v", externalMessage),
		externalMessage,
		codes.NotFound)
}

func NewResourceNotFoundError(resourceType string, resourceName string) *UserError {
	externalMessage := fmt.Sprintf("%s %s not found.", resourceType, resourceName)
	return newUserError(
		fmt.Errorf("ResourceNotFoundError: %v", externalMessage),
		externalMessage,
		codes.NotFound)
}

func NewResourcesNotFoundError(resourceTypesFormat string, resourceNames ...interface{}) *UserError {
	externalMessage := fmt.Sprintf("%s not found.", fmt.Sprintf(resourceTypesFormat, resourceNames...))
	return newUserError(
		fmt.Errorf("ResourceNotFoundError: %v", externalMessage),
		externalMessage,
		codes.NotFound)
}

func NewInvalidInputError(messageFormat string, a ...interface{}) *UserError {
	message := fmt.Sprintf(messageFormat, a...)
	return newUserError(errors.Errorf("Invalid input error: %v", message), message, codes.InvalidArgument)
}

func NewInvalidInputErrorWithDetails(err error, externalMessage string) *UserError {
	return newUserError(
		errors.Wrapf(err, "InvalidInputError: %v", externalMessage),
		externalMessage,
		codes.InvalidArgument)
}

func NewAlreadyExistError(messageFormat string, a ...interface{}) *UserError {
	message := fmt.Sprintf(messageFormat, a...)
	return newUserError(errors.Errorf("Already exist error: %v", message), message, codes.AlreadyExists)
}

func NewBadRequestError(err error, externalFormat string, a ...interface{}) *UserError {
	externalMessage := fmt.Sprintf(externalFormat, a...)
	return newUserError(
		errors.Wrapf(err, "BadRequestError: %v", externalMessage),
		externalMessage,
		codes.Aborted)
}

func NewUnauthenticatedError(err error, externalFormat string, a ...interface{}) *UserError {
	externalMessage := fmt.Sprintf(externalFormat, a...)
	return newUserError(
		errors.Wrapf(err, "Unauthenticated: %v", externalMessage),
		externalMessage,
		codes.Unauthenticated)
}

func NewPermissionDeniedError(err error, externalFormat string, a ...interface{}) *UserError {
	externalMessage := fmt.Sprintf(externalFormat, a...)
	return newUserError(
		errors.Wrapf(err, "PermissionDenied: %v", externalMessage),
		externalMessage,
		codes.PermissionDenied)
}

func (e *UserError) ExternalMessage() string {
	return e.externalMessage
}

func (e *UserError) ExternalStatusCode() codes.Code {
	return e.externalStatusCode
}

func (e *UserError) Error() string {
	return e.internalError.Error()
}

func (e *UserError) Cause() error {
	return e.internalError
}

func (e *UserError) String() string {
	return fmt.Sprintf("%v (code: %v): %+v", e.externalMessage, e.externalStatusCode,
		e.internalError)
}

func (e *UserError) ErrorStringWithoutStackTrace() string {
	return fmt.Sprintf("%v: %v", e.externalMessage, e.internalError)
}

// GRPCStatus implements `GRPCStatus` to make sure `FromError` in grpc-go can honor the code.
// Otherwise, it will always return codes.Unknown(2).
// https://github.com/grpc/grpc-go/blob/2c0949c22d46095edc579d9e66edcd025192b98c/status/status.go#L91-L92
func (e *UserError) GRPCStatus() *status.Status {
	return status.New(e.externalStatusCode, e.ErrorStringWithoutStackTrace())
}

func (e *UserError) wrapf(format string, args ...interface{}) *UserError {
	return newUserError(errors.Wrapf(e.internalError, format, args...),
		e.externalMessage, e.externalStatusCode)
}

func (e *UserError) wrap(message string) *UserError {
	return newUserError(errors.Wrap(e.internalError, message),
		e.externalMessage, e.externalStatusCode)
}

func (e *UserError) Log() {
	switch e.externalStatusCode {
	case codes.Aborted, codes.InvalidArgument, codes.NotFound, codes.Internal:
		klog.Infof("%+v", e.internalError)
	default:
		klog.Errorf("%+v", e.internalError)
	}
}

func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}

	var userError *UserError
	if errors.As(err, &userError) {
		return userError.wrapf(format, args...)
	}

	return errors.Wrapf(err, format, args...)
}

func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}

	var userErr *UserError
	if errors.As(err, &userErr) {
		return userErr.wrap(message)
	}

	return errors.Wrapf(err, message)
}

func LogError(err error) {
	// Check if the error is of type *UserError, even if it's wrapped
	var userError *UserError
	if errors.As(err, &userError) {
		// If it's a *UserError, log it using the Log method
		userError.Log()
	} else {
		// For all other errors, log all the details
		klog.Errorf("InternalError: %+v", err)
	}
}

// TerminateIfError Check if error is nil. Terminate if not.
func TerminateIfError(err error) {
	if err != nil {
		klog.Fatalf("%v", err)
	}
}

// IsNotFound returns whether an error indicates that a resource was "not found".
func IsNotFound(err error) bool {
	return reasonForError(err) == k8metav1.StatusReasonNotFound
}

// IsUserErrorCodeMatch returns whether the error is a user error with specified code.
func IsUserErrorCodeMatch(err error, code codes.Code) bool {
	var userError *UserError
	if errors.As(err, &userError) {
		return userError.externalStatusCode == code
	}
	return false
}

// ReasonForError returns the HTTP status for a particular error.
func reasonForError(err error) k8metav1.StatusReason {
	var statusErr *k8errors.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status().Reason
	}

	var apiStatus k8errors.APIStatus
	if errors.As(err, &apiStatus) {
		return apiStatus.Status().Reason
	}

	return k8metav1.StatusReasonUnknown
}
