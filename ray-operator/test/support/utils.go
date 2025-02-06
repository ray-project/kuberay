package support

import (
	"io/fs"
	"os"
	"path"

	"github.com/stretchr/testify/require"
)

func Ptr[T any](v T) *T {
	return &v
}

type OutputType string

const (
	Log OutputType = "log"
)

func WriteToOutputDir(t Test, fileName string, fileType OutputType, data []byte) {
	t.T().Helper()
	err := os.WriteFile(path.Join(t.OutputDir(), fileName+"."+string(fileType)), data, fs.ModePerm)
	require.NoError(t.T(), err)
}
