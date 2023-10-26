package e2e

import (
	"embed"

	"github.com/onsi/gomega"
	"github.com/ray-project/kuberay/ray-operator/test/support"
)

//go:embed *.py
var files embed.FS

func ReadFile(t support.Test, fileName string) []byte {
	t.T().Helper()
	file, err := files.ReadFile(fileName)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	return file
}
