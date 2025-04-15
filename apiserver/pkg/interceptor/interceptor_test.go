package interceptor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	klog "k8s.io/klog/v2"
)

// mockHandler simulates a gRPC handler for testing
type mockHandler struct {
	called    bool
	returnErr error
}

func (h *mockHandler) Handle(ctx context.Context, req interface{}) (interface{}, error) {
	h.called = true
	return "test_response", h.returnErr
}

func TestAPIServerInterceptor(t *testing.T) {
	tests := []struct {
		name          string
		handler       *mockHandler
		expectedResp  interface{}
		expectedError error
	}{
		{
			name:          "successful handler execution",
			handler:       &mockHandler{returnErr: nil},
			expectedResp:  "test_response",
			expectedError: nil,
		},
		{
			name:          "handler returns error",
			handler:       &mockHandler{returnErr: errors.New("handler error")},
			expectedResp:  "test_response",
			expectedError: errors.New("handler error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context and request
			ctx := context.Background()
			req := "test_request"
			info := &grpc.UnaryServerInfo{
				FullMethod: "TestMethod",
			}

			// Call the interceptor
			resp, err := APIServerInterceptor(
				ctx,
				req,
				info,
				func(ctx context.Context, req interface{}) (interface{}, error) {
					return tt.handler.Handle(ctx, req)
				},
			)

			// Verify handler was called
			assert.True(t, tt.handler.called, "handler should have been called")

			// Verify response
			assert.Equal(t, tt.expectedResp, resp, "response should match expected")

			// Verify error
			if tt.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.expectedError.Error(), "A matching error is expected")
			}
		})
	}
}

// TestAPIServerInterceptorContextPassing ensures context is properly passed through
func TestAPIServerInterceptorContextPassing(t *testing.T) {
	ctx := context.WithValue(context.Background(), "test_key", "test_value")
	handler := &mockHandler{}
	info := &grpc.UnaryServerInfo{FullMethod: "TestMethod"}

	_, _ = APIServerInterceptor(
		ctx,
		"test_request",
		info,
		func(receivedCtx context.Context, req interface{}) (interface{}, error) {
			// Verify context value is passed through
			assert.Equal(t, "test_value", receivedCtx.Value("test_key"))
			return handler.Handle(receivedCtx, req)
		},
	)
}

// TestAPIServerInterceptorLogging verifies that the interceptor logs start, finish, and warning messages.
func TestAPIServerInterceptorLogging(t *testing.T) {
	tests := []struct {
		name           string
		handlerErr     error
		expectedLogs   []string
		unexpectedLogs []string
	}{
		{
			name:       "successful execution - info logs",
			handlerErr: nil,
			expectedLogs: []string{
				"TestLoggingMethod handler starting",
				"TestLoggingMethod handler finished",
			},
			unexpectedLogs: []string{
				"handler error",
			},
		},
		{
			name:       "error execution - warning logs",
			handlerErr: errors.New("handler error"),
			expectedLogs: []string{
				"TestLoggingMethod handler starting",
				"handler error",
				"TestLoggingMethod handler finished",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Redirect stderr to capture klog output
			originalStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Restore stderr after the test
			defer func() {
				klog.Flush()
				os.Stderr = originalStderr
			}()

			ctx := context.Background()
			handler := &mockHandler{returnErr: tt.handlerErr}
			info := &grpc.UnaryServerInfo{FullMethod: "TestLoggingMethod"}

			_, err := APIServerInterceptor(
				ctx,
				"test_request",
				info,
				func(receivedCtx context.Context, req interface{}) (interface{}, error) {
					return handler.Handle(receivedCtx, req)
				},
			)

			if tt.handlerErr != nil {
				require.EqualError(t, tt.handlerErr, err.Error(), "A matching error is expected")
			} else {
				require.NoError(t, err)
			}

			// Close the write end of the pipe and read the captured output
			w.Close()
			var buf bytes.Buffer
			io.Copy(&buf, r)
			logOutput := buf.String()

			for _, expectedLog := range tt.expectedLogs {
				assert.True(t,
					strings.Contains(logOutput, expectedLog),
					"Log output should contain '%s'\nGot logs:\n%s",
					expectedLog,
					logOutput,
				)
			}

			for _, unexpectedLog := range tt.unexpectedLogs {
				assert.False(t,
					strings.Contains(logOutput, unexpectedLog),
					"Log output should not contain '%s'\nGot logs:\n%s",
					unexpectedLog,
					logOutput,
				)
			}

			assert.True(t, handler.called, "handler should have been called")
		})
	}
}
