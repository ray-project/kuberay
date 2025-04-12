package interceptor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
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
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
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

// TestAPIServerInterceptorLogging verifies logging behavior
func TestAPIServerInterceptorLogging(t *testing.T) {
	// This test mainly ensures the code paths with logging are executed
	// Since klog is a global logger, we can't easily verify the output
	// but we can ensure the code executes without panics
	ctx := context.Background()
	handler := &mockHandler{returnErr: errors.New("test error")}
	info := &grpc.UnaryServerInfo{FullMethod: "TestMethod"}

	_, err := APIServerInterceptor(
		ctx,
		"test_request",
		info,
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return handler.Handle(ctx, req)
		},
	)

	assert.Error(t, err)
	assert.Equal(t, "test error", err.Error())
}
