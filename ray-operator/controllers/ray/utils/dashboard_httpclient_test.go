package utils

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jarcoal/httpmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const (
	runtimeEnvStr = `working_dir: "./"
pip:
- requests==2.26.0
- pendulum==2.1.2
conda:
  dependencies:
  - pytorch
  - torchvision
  - pip
  - pip:
    - pendulum
eager_install: false`
)

var _ = Describe("RayFrameworkGenerator", func() {
	var (
		rayJob             *rayv1.RayJob
		expectJobId        string
		errorJobId         string
		rayDashboardClient *RayDashboardClient
	)

	BeforeEach(func() {
		expectJobId = "raysubmit_test001"
		rayJob = &rayv1.RayJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rayjob-sample",
				Namespace: "default",
			},
			Spec: rayv1.RayJobSpec{
				Entrypoint: "python samply.py",
				Metadata: map[string]string{
					"owner": "test1",
				},
				RuntimeEnvYAML: runtimeEnvStr,
			},
		}

		rayDashboardClient = &RayDashboardClient{}
		err := rayDashboardClient.InitClient(context.Background(), "127.0.0.1:8090", nil)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Test ConvertRayJobToReq", func() {
		rayJobRequest, err := ConvertRayJobToReq(rayJob)
		Expect(err).ToNot(HaveOccurred())
		Expect(rayJobRequest.RuntimeEnv).To(HaveLen(4))
		Expect(rayJobRequest.RuntimeEnv["working_dir"]).To(Equal("./"))
	})

	It("Test ConvertRayJobToReq with EntrypointResources", func() {
		rayJobRequest, err := ConvertRayJobToReq(&rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				EntrypointResources: `{"r1": 0.1, "r2": 0.2}`,
				EntrypointNumCpus:   1.1,
				EntrypointNumGpus:   2.2,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rayJobRequest.NumCpus).To(Equal(float32(1.1)))
		Expect(rayJobRequest.NumGpus).To(Equal(float32(2.2)))
		Expect(rayJobRequest.Resources).To(Equal(map[string]float32{"r1": 0.1, "r2": 0.2}))
	})

	It("Test ConvertRayJobToReq with invalid EntrypointResources", func() {
		_, err := ConvertRayJobToReq(&rayv1.RayJob{
			Spec: rayv1.RayJobSpec{
				EntrypointResources: `{"r1": "string"}`,
			},
		})
		Expect(err).Should(MatchError(ContainSubstring("json: cannot unmarshal")))
	})

	It("Test submitting/getting rayJob", func() {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		httpmock.RegisterResponder("POST", rayDashboardClient.dashboardURL+JobPath,
			func(_ *http.Request) (*http.Response, error) {
				body := &RayJobResponse{
					JobId: expectJobId,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})
		httpmock.RegisterResponder("GET", rayDashboardClient.dashboardURL+JobPath+expectJobId,
			func(_ *http.Request) (*http.Response, error) {
				body := &RayJobInfo{
					JobStatus:  rayv1.JobStatusRunning,
					Entrypoint: rayJob.Spec.Entrypoint,
					Metadata:   rayJob.Spec.Metadata,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})
		httpmock.RegisterResponder("GET", rayDashboardClient.dashboardURL+JobPath+errorJobId,
			func(_ *http.Request) (*http.Response, error) {
				// return a string in the body
				return httpmock.NewStringResponse(200, "Ray misbehaved and sent string, not JSON"), nil
			})

		jobId, err := rayDashboardClient.SubmitJob(context.TODO(), rayJob)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobId).To(Equal(expectJobId))

		rayJobInfo, err := rayDashboardClient.GetJobInfo(context.TODO(), jobId)
		Expect(err).ToNot(HaveOccurred())
		Expect(rayJobInfo.Entrypoint).To(Equal(rayJob.Spec.Entrypoint))
		Expect(rayJobInfo.JobStatus).To(Equal(rayv1.JobStatusRunning))

		_, err = rayDashboardClient.GetJobInfo(context.TODO(), errorJobId)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("GetJobInfo fail"))
		Expect(err.Error()).To(ContainSubstring("Ray misbehaved"))
	})

	It("Test stop job", func() {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		httpmock.RegisterResponder("POST", rayDashboardClient.dashboardURL+JobPath+"stop-job-1/stop",
			func(_ *http.Request) (*http.Response, error) {
				body := &RayJobStopResponse{
					Stopped: true,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})

		err := rayDashboardClient.StopJob(context.TODO(), "stop-job-1")
		Expect(err).ToNot(HaveOccurred())
	})

	It("Test stop succeeded job", func() {
		// StopJob only returns an error when JobStatus is not in terminated states (STOPPED / SUCCEEDED / FAILED)
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		httpmock.RegisterResponder("POST", rayDashboardClient.dashboardURL+JobPath+"stop-job-1/stop",
			func(_ *http.Request) (*http.Response, error) {
				body := &RayJobStopResponse{
					Stopped: false,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})
		httpmock.RegisterResponder("GET", rayDashboardClient.dashboardURL+JobPath+"stop-job-1",
			func(_ *http.Request) (*http.Response, error) {
				body := &RayJobInfo{
					JobStatus:  rayv1.JobStatusSucceeded,
					Entrypoint: rayJob.Spec.Entrypoint,
					Metadata:   rayJob.Spec.Metadata,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})

		err := rayDashboardClient.StopJob(context.TODO(), "stop-job-1")
		Expect(err).ToNot(HaveOccurred())
	})
})
