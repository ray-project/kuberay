package support

import (
	"io/fs"
	"os"
	"path"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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
	assert.NoError(t.T(), err)
}

// Copy from ray-operator/main.go
func CacheSelectors() (map[client.Object]cache.ByObject, error) {
	label, err := labels.NewRequirement(utils.KubernetesCreatedByLabelKey, selection.Equals, []string{utils.ComponentName})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*label)

	return map[client.Object]cache.ByObject{
		&batchv1.Job{}: {Label: selector},
	}, nil
}
