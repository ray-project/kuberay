package support

import (
	"io/fs"
	"os"
	"path"

	"github.com/onsi/gomega"
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
	t.Expect(os.WriteFile(path.Join(t.OutputDir(), fileName+"."+string(fileType)), data, fs.ModePerm)).
		To(gomega.Succeed())
}
