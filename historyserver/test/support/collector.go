package support

import (
	"fmt"
	"path/filepath"
	"strings"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// VerifySessionDirectoriesExist checks that the session directory exists under
// /tmp/ray/{prev-logs,persist-complete-logs}/<sessionID> in the head pod.
func VerifySessionDirectoriesExist(test Test, g *WithT, rayCluster *rayv1.RayCluster, sessionID string) {
	dirs := []string{"prev-logs", "persist-complete-logs"}
	for _, dir := range dirs {
		dirPath := filepath.Join("/tmp/ray", dir, sessionID)
		LogWithTimestamp(test.T(), "Checking if session directory %s exists in %s", sessionID, dirPath)
		g.Eventually(func(gg Gomega) {
			headPod, err := GetHeadPod(test, rayCluster)
			gg.Expect(err).NotTo(HaveOccurred())
			checkDirExistsCmd := fmt.Sprintf("test -d %s && echo 'exists' || echo 'not found'", dirPath)
			stdout, stderr := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", checkDirExistsCmd})
			gg.Expect(stderr.String()).To(BeEmpty())
			gg.Expect(strings.TrimSpace(stdout.String())).To(Equal("exists"),
				"Session directory %s should exist in %s", sessionID, dirPath)
		}, TestTimeoutMedium).Should(Succeed(), "Session directory %s should exist in %s", sessionID, dirPath)
	}
}
