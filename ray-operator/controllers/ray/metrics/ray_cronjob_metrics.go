package metrics

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// RayCronJobMetricsManager implements the prometheus.Collector interface to collect RayCronJob metrics.
type RayCronJobMetricsManager struct {
	// rayCronJobInfo is a gauge metric that provides metadata about RayCronJob custom resources.
	rayCronJobInfo *prometheus.Desc

	// rayCronJobActiveJobs is a gauge metric that indicates the number of currently active jobs for a RayCronJob.
	rayCronJobActiveJobs *prometheus.Desc

	// rayCronJobLastScheduleTime is a gauge metric for the Unix timestamp of the last successfully scheduled job.
	rayCronJobLastScheduleTime *prometheus.Desc

	// rayCronJobLastSuccessfulTime is a gauge metric for the Unix timestamp of the last successfully completed job.
	rayCronJobLastSuccessfulTime *prometheus.Desc

	client client.Client
	log    logr.Logger
}

// NewRayCronJobMetricsManager creates a new RayCronJobMetricsManager instance.
func NewRayCronJobMetricsManager(ctx context.Context, client client.Client) *RayCronJobMetricsManager {
	return &RayCronJobMetricsManager{
		rayCronJobInfo: prometheus.NewDesc(
			"kuberay_cronjob_info",
			"Metadata information about RayCronJob custom resources.",
			[]string{"name", "namespace", "schedule", "concurrency_policy", "suspend"},
			nil,
		),
		rayCronJobActiveJobs: prometheus.NewDesc(
			"kuberay_cronjob_active_jobs",
			"The number of currently active jobs for a RayCronJob.",
			[]string{"name", "namespace"},
			nil,
		),
		rayCronJobLastScheduleTime: prometheus.NewDesc(
			"kuberay_cronjob_last_schedule_time_seconds",
			"The Unix timestamp of the last time a job was successfully scheduled.",
			[]string{"name", "namespace"},
			nil,
		),
		rayCronJobLastSuccessfulTime: prometheus.NewDesc(
			"kuberay_cronjob_last_successful_time_seconds",
			"The Unix timestamp of the last time a job successfully completed.",
			[]string{"name", "namespace"},
			nil,
		),
		client: client,
		log:    ctrl.LoggerFrom(ctx),
	}
}

// Describe implements the prometheus.Collector interface.
func (r *RayCronJobMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	ch <- r.rayCronJobInfo
	ch <- r.rayCronJobActiveJobs
	ch <- r.rayCronJobLastScheduleTime
	ch <- r.rayCronJobLastSuccessfulTime
}

// Collect implements the prometheus.Collector interface.
func (r *RayCronJobMetricsManager) Collect(ch chan<- prometheus.Metric) {
	var rayCronJobList rayv1.RayCronJobList
	if err := r.client.List(context.Background(), &rayCronJobList); err != nil {
		r.log.Error(err, "Failed to list RayCronJob resources for metrics collection")
		return
	}

	for _, rayCronJob := range rayCronJobList.Items {
		r.collectRayCronJobMetrics(&rayCronJob, ch)
	}
}

// collectRayCronJobMetrics gathers all metrics for a single RayCronJob.
func (r *RayCronJobMetricsManager) collectRayCronJobMetrics(rayCronJob *rayv1.RayCronJob, ch chan<- prometheus.Metric) {
	suspended := "false"
	if rayCronJob.Spec.Suspend != nil && *rayCronJob.Spec.Suspend {
		suspended = "true"
	}

	// kuberay_cronjob_info
	ch <- prometheus.MustNewConstMetric(
		r.rayCronJobInfo,
		prometheus.GaugeValue,
		1,
		rayCronJob.Name,
		rayCronJob.Namespace,
		rayCronJob.Spec.Schedule,
		string(rayCronJob.Spec.ConcurrencyPolicy),
		suspended,
	)

	// kuberay_cronjob_active_jobs
	ch <- prometheus.MustNewConstMetric(
		r.rayCronJobActiveJobs,
		prometheus.GaugeValue,
		float64(len(rayCronJob.Status.Active)),
		rayCronJob.Name,
		rayCronJob.Namespace,
	)

	// kuberay_cronjob_last_schedule_time_seconds
	if rayCronJob.Status.LastScheduleTime != nil {
		ch <- prometheus.MustNewConstMetric(
			r.rayCronJobLastScheduleTime,
			prometheus.GaugeValue,
			float64(rayCronJob.Status.LastScheduleTime.Time.Unix()),
			rayCronJob.Name,
			rayCronJob.Namespace,
		)
	}

	// kuberay_cronjob_last_successful_time_seconds
	if rayCronJob.Status.LastSuccessfulTime != nil {
		ch <- prometheus.MustNewConstMetric(
			r.rayCronJobLastSuccessfulTime,
			prometheus.GaugeValue,
			float64(rayCronJob.Status.LastSuccessfulTime.Time.Unix()),
			rayCronJob.Name,
			rayCronJob.Namespace,
		)
	}
}
