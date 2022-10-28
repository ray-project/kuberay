package utils

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/jarcoal/httpmock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

const (
	runtimeEnvStr = "{\n    \"working_dir\": \"./\",\n    \"pip\": [\n        \"requests==2.26.0\",\n        \"pendulum==2.1.2\"\n    ],\n    \"conda\": {\n        \"dependencies\": [\n            \"pytorch\",\n            \"torchvision\",\n            \"pip\",\n            {\n                \"pip\": [\n                    \"pendulum\"\n                ]\n            }\n        ]\n    },\n    \"eager_install\": false\n}"
)

var _ = Describe("RayFrameworkGenerator", func() {
	var (
		rayJob             *rayv1alpha1.RayJob
		expectJobId        string
		rayDashboardClient *RayDashboardClient
	)

	BeforeEach(func() {
		expectJobId = "raysubmit_test001"
		encodedRuntimeEnv := base64.StdEncoding.EncodeToString([]byte(runtimeEnvStr))
		rayJob = &rayv1alpha1.RayJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rayjob-sample",
				Namespace: "default",
			},
			Spec: rayv1alpha1.RayJobSpec{
				Entrypoint: "python samply.py",
				Metadata: map[string]string{
					"owner": "test1",
				},
				RuntimeEnv: encodedRuntimeEnv,
			},
		}
		rayDashboardClient = &RayDashboardClient{}
		rayDashboardClient.InitClient("127.0.0.1:8090")
	})

	It("Test ConvertRayJobToReq", func() {
		rayJobRequest, err := ConvertRayJobToReq(rayJob)
		Expect(err).To(BeNil())
		Expect(len(rayJobRequest.RuntimeEnv)).To(Equal(4))
		Expect(rayJobRequest.RuntimeEnv["working_dir"]).To(Equal("./"))
	})

	It("Test submitting/getting rayJob", func() {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		httpmock.RegisterResponder("POST", rayDashboardClient.dashboardURL+JobPath,
			func(req *http.Request) (*http.Response, error) {
				body := &RayJobResponse{
					JobId: expectJobId,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})
		httpmock.RegisterResponder("GET", rayDashboardClient.dashboardURL+JobPath+expectJobId,
			func(req *http.Request) (*http.Response, error) {
				body := &RayJobInfo{
					JobStatus:  rayv1alpha1.JobStatusRunning,
					Entrypoint: rayJob.Spec.Entrypoint,
					Metadata:   rayJob.Spec.Metadata,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})

		jobId, err := rayDashboardClient.SubmitJob(rayJob, &ctrl.Log)
		Expect(err).To(BeNil())
		Expect(jobId).To(Equal(expectJobId))

		rayJobInfo, err := rayDashboardClient.GetJobInfo(jobId)
		Expect(err).To(BeNil())
		Expect(rayJobInfo.Entrypoint).To(Equal(rayJob.Spec.Entrypoint))
		Expect(rayJobInfo.JobStatus).To(Equal(rayv1alpha1.JobStatusRunning))
	})

	It("Test stop job", func() {
		httpmock.Activate()
		defer httpmock.DeactivateAndReset()
		httpmock.RegisterResponder("POST", rayDashboardClient.dashboardURL+JobPath+"stop-job-1/stop",
			func(req *http.Request) (*http.Response, error) {
				body := &RayJobStopResponse{
					Stopped: true,
				}
				bodyBytes, _ := json.Marshal(body)
				return httpmock.NewBytesResponse(200, bodyBytes), nil
			})

		err := rayDashboardClient.StopJob("stop-job-1", &ctrl.Log)
		Expect(err).To(BeNil())
	})
})
