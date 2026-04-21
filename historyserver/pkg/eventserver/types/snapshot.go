package types

// SessionSnapshot is an immutable, per-session aggregate of processed events.
//
// A snapshot is produced once per session by EventProcessor.Process() from the
// raw events under <sessionID>/ in object storage, and persisted by
// SnapshotStore under processed/<sessionID>/. The persisted layout is:
//
// TODO(jwj): Illustrate processed/ layout.
// processed/<sessionID>/
// ... tbd

type SessionSnapshot struct {
	// SessionID is the fully-qualified session identifier:
	// TODO(jwj): Add session ID example.
	SessionID string `json:"session_id"`

	// Nodes maps NodeID to Node.
	Nodes map[string]Node `json:"nodes"`

	// Tasks maps TaskID to a list of Task attempts.
	Tasks map[string][]Task `json:"tasks"`

	// Actors maps ActorID to Actor.
	Actors map[string]Actor `json:"actors"`

	// Jobs maps JobID to Job.
	Jobs map[string]Job `json:"jobs"`

	// LogEvents maps JobID to a map of EventID to LogEvent, feeding the /events endpoint.
	// The "global" JobID key holds events without a JobID.
	LogEvents map[string]map[string]LogEvent `json:"log_events"`
}
