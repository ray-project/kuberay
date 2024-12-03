package support

import (
	"io/fs"
	"os"
	"path"

	"github.com/stretchr/testify/assert"
)

func Ptr[T any](v T) *T {
	return &v
}

type OutputType string

const (
	Log OutputType = "log"
	// MultiKueueController represents the vaue of the MultiKueue controller
	MultiKueueController = "kueue.x-k8s.io/multikueue"
)

func WriteToOutputDir(t Test, fileName string, fileType OutputType, data []byte) {
	t.T().Helper()
	err := os.WriteFile(path.Join(t.OutputDir(), fileName+"."+string(fileType)), data, fs.ModePerm)
	assert.NoError(t.T(), err)
}
