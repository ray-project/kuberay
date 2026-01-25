package types

// ChromeTraceEvent represents a trace event in Chrome Tracing Format
type ChromeTraceEvent struct {
	Category  string                 `json:"cat,omitempty"`
	Name      string                 `json:"name"`
	PID       int                    `json:"pid"`
	TID       *int                   `json:"tid"`
	Timestamp *float64               `json:"ts,omitempty"`
	Duration  *float64               `json:"dur,omitempty"` // microseconds
	Color     string                 `json:"cname,omitempty"`
	Args      map[string]interface{} `json:"args"`
	Phase     string                 `json:"ph"`
}
