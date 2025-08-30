package common

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// Generate a string of length 200.
func longString(t *testing.T) string {
	var b bytes.Buffer
	for i := 0; i < 200; i++ {
		b.WriteString("a")
	}
	result := b.String()
	// Confirm length.
	assert.Len(t, result, 200)
	return result
}

// Clip the above string using utils.CheckName
// to a string of length 50.
func shortString(ctx context.Context, t *testing.T) string {
	result := utils.CheckName(ctx, longString(t))
	// Confirm length.
	assert.Len(t, result, 50)
	return result
}
