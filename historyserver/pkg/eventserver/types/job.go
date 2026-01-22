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

type DriverInfo struct {
	ID            string `json:"id"`
	NodeIPAddress string `json:"nodeIpAddress"`
	NodeID        string `json:"nodeId"`
	PID           string `json:"pid"`
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

type Job struct {
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
	Metadata               map[string]string `json:"metadata"`
	RuntimeEnv             map[string]string `json:"runtimeEnv"`
	DriverInfo             DriverInfo        `json:"driverInfo"`
	DriverAgentHttpAddress string            `json:"driverAgentHttpAddress"`
	DriverNodeID           string            `json:"drivernodeId"`
	DriverExitCode         int               `json:"driverExitCode"`

	// StateTransitions is the state (connectivity of the driver) job timeline
	StateTransitions []JobStateTransition

	// StatusTransitions is the status (progress) of the job timeline
	StatusTransitions []JobStatusTransition
}

type JobMap struct {
	JobMap map[string]Job
	Mu     sync.Mutex
}

func (j *JobMap) Lock() {
	j.Mu.Lock()
}

func (j *JobMap) UnLock() {
	j.Mu.Unlock()
}

func NewJobMap() *JobMap {
	return &JobMap{
		JobMap: make(map[string]Job),
	}
}

type ClusterJobMap struct {
	ClusterJobMap map[string]*JobMap
	Mu            sync.RWMutex
}

func (c *ClusterJobMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterJobMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterJobMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterJobMap) Unlock() {
	c.Mu.Unlock()
}

func (c *ClusterJobMap) GetOrCreateJobMap(clusterName string) *JobMap {
	c.Lock()
	defer c.Unlock()

	jobMap, exists := c.ClusterJobMap[clusterName]
	if !exists {
		jobMap = NewJobMap()
		c.ClusterJobMap[clusterName] = jobMap
	}

	return jobMap
}

// CreateOrMergeJob will find or create the job and runs the merge function.
// There are two fields that is part of the Job object that can transition.
// The State and Status, one representing the driver lifecycle and another
// represents the status of the progress of the job.
func (j *JobMap) CreateOrMergeJob(jobId string, mergeFn func(*Job)) {
	j.Lock()
	defer j.UnLock()

	job, exist := j.JobMap[jobId]
	if !exist {
		// Job does not exist, creating new with JobID
		newJob := Job{JobID: jobId}
		mergeFn(&newJob)
		j.JobMap[jobId] = newJob
		return
	}

	mergeFn(&job)
	j.JobMap[jobId] = job
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
	if len(j.Metadata) > 0 {
		cp.Metadata = make(map[string]string, len(j.Metadata))
		for k, v := range j.Metadata {
			cp.Metadata[k] = v
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
