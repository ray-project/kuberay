package types

import (
	"sync"
	"time"
)

// JobStatus represents the job submission progress and execution status
type JobStatus string

const (
	SUCCEEDED JobStatus = "SUCCEEDED"
	PENDING   JobStatus = "PENDING"
	STOPPED   JobStatus = "STOPPED"
	// FAILED is already used in the package, using JOBFAILED
	JOBFAILED JobStatus = "FAILED"
	// RUNNING is already used in the package, using JOBRUNNING
	JOBRUNNING JobStatus = "RUNNING"
)

type JobStatusTransition struct {
	Status    JobStatus
	Timestamp time.Time
}

// JobState represents the literal job driver process and connectivity
type JobState string

const (
	UNSPECIFIED JobState = "UNSPECIFIED"
	CREATED     JobState = "CREATED"
	// FINISHED is already used in package, using JOBFINSIHED
	JOBFINISHED JobState = "FINISHED"
)

type JobStateTransition struct {
	State     JobState
	Timestamp time.Time
}

type DriverJobConfig struct {
	Metadata             map[string]string `json:"metadata"`
	SerializedRuntimeEnv string            `json:"serializedRuntimeEnv"`
}

type Job struct {
	// JobID will be in Hex, as that is the output format when calling /api/jobs/
	JobID                  string            `json:"jobId"`
	SubmissionID           string            `json:"submissionId"`
	JobType                string            `json:"type"`
	Status                 JobStatus         `json:"status"`
	State                  JobState          `json:"state"`
	EntryPoint             string            `json:"entrypoint"`
	Message                string            `json:"message"`
	ErrorType              string            `json:"errorType"`
	StartTime              time.Time         `json:"startTime"`
	EndTime                time.Time         `json:"endTime"`
	Config                 DriverJobConfig   `json:"config"`
	RuntimeEnv             map[string]string `json:"runtimeEnv"`
	DriverAgentHttpAddress string            `json:"driverAgentHttpAddress"`
	DriverNodeID           string            `json:"driverNodeId"`
	DriverNodeIPAddress    string            `json:"driverNodeIpAddress"`
	DriverExitCode         int               `json:"driverExitCode"`
	DriverPID              string            `json:"driverPid"`

	// StateTransitions is the state (connectivity of the driver) job timeline
	StateTransitions []JobStateTransition

	// StatusTransitions is the status (progress) of the job timeline
	StatusTransitions []JobStatusTransition
}

type JobMap struct {
	jobMap map[string]Job
	mu     sync.Mutex
}

func NewJobMap() *JobMap {
	return &JobMap{
		jobMap: make(map[string]Job),
	}
}

type ClusterJobMap struct {
	clusterJobMap map[string]*JobMap
	mu            sync.RWMutex
}

func NewClusterJobMap() *ClusterJobMap {
	return &ClusterJobMap{
		clusterJobMap: make(map[string]*JobMap),
	}
}

func (c *ClusterJobMap) GetOrCreateJobMap(clusterName string) *JobMap {
	c.mu.Lock()
	defer c.mu.Unlock()

	jobMap, exists := c.clusterJobMap[clusterName]
	if !exists {
		jobMap = NewJobMap()
		c.clusterJobMap[clusterName] = jobMap
	}

	return jobMap
}

// GetJobsMap returns a deep copy of all jobs for a cluster session.
func (c *ClusterJobMap) GetJobsMap(clusterName string) map[string]Job {
	c.mu.RLock()
	jobMap, ok := c.clusterJobMap[clusterName]
	c.mu.RUnlock()
	if !ok {
		return map[string]Job{}
	}

	return jobMap.GetJobsMap()
}

// GetJobByJobID returns a deep copy of a job by ID in a cluster session.
func (c *ClusterJobMap) GetJobByJobID(clusterName, jobID string) (Job, bool) {
	c.mu.RLock()
	jobMap, ok := c.clusterJobMap[clusterName]
	c.mu.RUnlock()
	if !ok {
		return Job{}, false
	}

	return jobMap.GetJobByJobID(jobID)
}

// CreateOrMergeJob will find or create the job and runs the merge function.
// There are two fields that is part of the Job object that can transition.
// The State and Status, one representing the driver lifecycle and another
// represents the status of the progress of the job.
func (j *JobMap) CreateOrMergeJob(jobId string, mergeFn func(*Job)) {
	j.mu.Lock()
	defer j.mu.Unlock()

	job, exist := j.jobMap[jobId]
	if !exist {
		// Job does not exist, creating new with JobID
		newJob := Job{JobID: jobId}
		mergeFn(&newJob)
		j.jobMap[jobId] = newJob
		return
	}

	mergeFn(&job)
	j.jobMap[jobId] = job
}

// GetJobsMap returns a deep copy of all jobs.
func (j *JobMap) GetJobsMap() map[string]Job {
	j.mu.Lock()
	defer j.mu.Unlock()

	jobs := make(map[string]Job, len(j.jobMap))
	for id, job := range j.jobMap {
		jobs[id] = job.DeepCopy()
	}

	return jobs
}

// GetJobByJobID returns a deep copy of a job by ID.
func (j *JobMap) GetJobByJobID(jobID string) (Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	job, ok := j.jobMap[jobID]
	if !ok {
		return Job{}, false
	}

	return job.DeepCopy(), true
}

// DeepCopy will return a deep copy of the Job
func (j Job) DeepCopy() Job {
	cp := j
	if len(j.StateTransitions) > 0 {
		cp.StateTransitions = make([]JobStateTransition, len(j.StateTransitions))
		copy(cp.StateTransitions, j.StateTransitions)
	}
	if len(j.StatusTransitions) > 0 {
		cp.StatusTransitions = make([]JobStatusTransition, len(j.StatusTransitions))
		copy(cp.StatusTransitions, j.StatusTransitions)
	}
	if len(j.Config.Metadata) > 0 {
		cp.Config.Metadata = make(map[string]string, len(j.Config.Metadata))
		for k, v := range j.Config.Metadata {
			cp.Config.Metadata[k] = v
		}
	}
	if len(j.RuntimeEnv) > 0 {
		cp.RuntimeEnv = make(map[string]string, len(j.RuntimeEnv))
		for k, v := range j.RuntimeEnv {
			cp.RuntimeEnv[k] = v
		}
	}

	return cp
}
