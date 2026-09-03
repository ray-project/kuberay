package utils

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/emicklei/go-restful/v3"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

// newListAPIRequest builds the kind of request the list API receives: a GET whose query
// string carries the options. url.Values is used instead of a raw string because the
// filter triples depend on the order of the repeated parameters, which url.Values keeps.
func newListAPIRequest(query url.Values) *restful.Request {
	return restful.NewRequest(httptest.NewRequest(http.MethodGet, "/api/v0/tasks?"+query.Encode(), nil))
}

// taskKeys renders each task as taskID#attempt, so that a single assertion covers both
// the ordering and the task_attempt tie-break that ApplyTaskFilters is responsible for.
func taskKeys(tasks []eventtypes.Task) []string {
	keys := make([]string, 0, len(tasks))
	for _, task := range tasks {
		keys = append(keys, fmt.Sprintf("%s#%d", task.TaskID, task.TaskAttempt))
	}
	return keys
}

// filterableField adapts a Task to the fieldGetter signature that ApplyFilter expects.
func filterableField(task eventtypes.Task, filterKey string) string {
	return task.GetFilterableFieldValue(filterKey)
}

// --- Tests ---

func TestParsePredicate(t *testing.T) {
	tests := []struct {
		scenario    string
		predicate   string
		expected    PredicateType
		expectError bool
	}{
		{scenario: "equal", predicate: "=", expected: PredicateEqual},
		{scenario: "not equal", predicate: "!=", expected: PredicateNotEqual},
		{scenario: "empty predicate is rejected", predicate: "", expectError: true},
		{scenario: "comparison operators are not supported", predicate: ">", expectError: true},
		{scenario: "double equals is not an alias for equal", predicate: "==", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			got, err := parsePredicate(tt.predicate)

			if tt.expectError {
				require.Error(t, err)
				assert.Empty(t, got, "a rejected predicate must not fall back to a usable one")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestParseOptionsFromReq(t *testing.T) {
	tests := []struct {
		scenario    string
		query       url.Values
		expected    ListAPIOptions
		expectedErr string
	}{
		{
			scenario: "no query parameters falls back to the defaults",
			query:    url.Values{},
			expected: ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: true, Filters: []Filter{}},
		},
		{
			scenario: "every option is read from the query string",
			query: url.Values{
				"limit":          {"5"},
				"timeout":        {"30"},
				"detail":         {"true"},
				"exclude_driver": {"false"},
			},
			expected: ListAPIOptions{Limit: 5, Timeout: 30, Detail: true, ExcludeDriver: false, Filters: []Filter{}},
		},
		{
			scenario: "empty values are skipped instead of parsed",
			query:    url.Values{"limit": {""}, "timeout": {""}, "detail": {""}, "exclude_driver": {""}},
			expected: ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: true, Filters: []Filter{}},
		},
		{
			scenario: "booleans accept the strconv shorthand",
			query:    url.Values{"detail": {"1"}, "exclude_driver": {"0"}},
			expected: ListAPIOptions{Limit: DefaultLimit, Detail: true, ExcludeDriver: false, Filters: []Filter{}},
		},
		{
			scenario: "filters are carried over into the options",
			query: url.Values{
				"filter_keys":       {"state"},
				"filter_predicates": {"="},
				"filter_values":     {"RUNNING"},
			},
			expected: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "state", FilterPredicate: PredicateEqual, FilterValue: "RUNNING"}},
			},
		},
		{
			scenario: "a zero limit is accepted and is not replaced by the default",
			query:    url.Values{"limit": {"0"}},
			expected: ListAPIOptions{Limit: 0, ExcludeDriver: true, Filters: []Filter{}},
		},
		{
			scenario: "a limit exactly at the ceiling is accepted",
			query:    url.Values{"limit": {"10000"}},
			expected: ListAPIOptions{Limit: RayMaxLimitFromAPIServer, ExcludeDriver: true, Filters: []Filter{}},
		},
		{
			scenario:    "a limit above the ceiling is rejected",
			query:       url.Values{"limit": {"10001"}},
			expectedErr: "limit cannot be greater than 10000",
		},
		{
			scenario:    "a negative limit is rejected",
			query:       url.Values{"limit": {"-1"}},
			expectedErr: "invalid limit: cannot be negative",
		},
		{
			scenario:    "a non-numeric limit is rejected",
			query:       url.Values{"limit": {"abc"}},
			expectedErr: "invalid limit",
		},
		{
			scenario:    "a negative timeout is rejected",
			query:       url.Values{"timeout": {"-1"}},
			expectedErr: "invalid timeout: cannot be negative",
		},
		{
			scenario:    "a non-boolean detail is rejected",
			query:       url.Values{"detail": {"yes"}},
			expectedErr: "invalid detail",
		},
		{
			scenario:    "a non-boolean exclude_driver is rejected",
			query:       url.Values{"exclude_driver": {"maybe"}},
			expectedErr: "invalid exclude_driver",
		},
		{
			scenario: "filter errors are wrapped so the caller knows which parameter failed",
			query: url.Values{
				"filter_keys":       {"state"},
				"filter_predicates": {">"},
				"filter_values":     {"RUNNING"},
			},
			expectedErr: "invalid filters parameter: invalid predicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			got, err := ParseOptionsFromReq(newListAPIRequest(tt.query))

			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGetFiltersFromReq(t *testing.T) {
	tests := []struct {
		scenario    string
		query       url.Values
		expected    []Filter
		expectedErr string
	}{
		{
			scenario: "no filter parameters yields an empty, non-nil slice",
			query:    url.Values{},
			expected: []Filter{},
		},
		{
			scenario: "the three parameters are paired index by index",
			query: url.Values{
				"filter_keys":       {"state", "job_id", "task_name"},
				"filter_predicates": {"=", "!=", "="},
				"filter_values":     {"RUNNING", "01000000", "train"},
			},
			expected: []Filter{
				{FilterKey: "state", FilterPredicate: PredicateEqual, FilterValue: "RUNNING"},
				{FilterKey: "job_id", FilterPredicate: PredicateNotEqual, FilterValue: "01000000"},
				{FilterKey: "task_name", FilterPredicate: PredicateEqual, FilterValue: "train"},
			},
		},
		{
			scenario: "the same key can appear twice with different predicates",
			query: url.Values{
				"filter_keys":       {"state", "state"},
				"filter_predicates": {"!=", "!="},
				"filter_values":     {"FAILED", "FINISHED"},
			},
			expected: []Filter{
				{FilterKey: "state", FilterPredicate: PredicateNotEqual, FilterValue: "FAILED"},
				{FilterKey: "state", FilterPredicate: PredicateNotEqual, FilterValue: "FINISHED"},
			},
		},
		{
			scenario: "an empty filter value is kept, since it is a legal thing to match on",
			query: url.Values{
				"filter_keys":       {"actor_id"},
				"filter_predicates": {"="},
				"filter_values":     {""},
			},
			expected: []Filter{{FilterKey: "actor_id", FilterPredicate: PredicateEqual, FilterValue: ""}},
		},
		{
			scenario: "fewer values than keys is rejected",
			query: url.Values{
				"filter_keys":       {"state", "job_id"},
				"filter_predicates": {"=", "="},
				"filter_values":     {"RUNNING"},
			},
			expectedErr: "filter_keys, filter_predicates, and filter_values must have the same length",
		},
		{
			scenario: "fewer predicates than keys is rejected",
			query: url.Values{
				"filter_keys":       {"state", "job_id"},
				"filter_predicates": {"="},
				"filter_values":     {"RUNNING", "01000000"},
			},
			expectedErr: "filter_keys, filter_predicates, and filter_values must have the same length",
		},
		{
			scenario: "an unsupported predicate is rejected",
			query: url.Values{
				"filter_keys":       {"state"},
				"filter_predicates": {"<"},
				"filter_values":     {"RUNNING"},
			},
			expectedErr: "invalid predicate: <",
		},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			got, err := getFiltersFromReq(newListAPIRequest(tt.query))

			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				assert.Nil(t, got, "a rejected filter set must not be partially returned")
				return
			}
			require.NoError(t, err)
			// Equal rather than ElementsMatch: the filters are applied in order.
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestApplyTaskFilters(t *testing.T) {
	driver := eventtypes.Task{TaskID: "d1", TaskType: eventtypes.DRIVER_TASK, JobID: "job1", State: eventtypes.RUNNING}
	running := eventtypes.Task{TaskID: "t1", TaskType: eventtypes.NORMAL_TASK, JobID: "job1", TaskName: "train", State: eventtypes.RUNNING}
	finished := eventtypes.Task{TaskID: "t2", TaskType: eventtypes.NORMAL_TASK, JobID: "job1", TaskName: "train", State: eventtypes.FINISHED}
	otherJob := eventtypes.Task{TaskID: "t3", TaskType: eventtypes.NORMAL_TASK, JobID: "job2", TaskName: "evaluate", State: eventtypes.RUNNING}
	actorMethod := eventtypes.Task{TaskID: "t4", TaskType: eventtypes.ACTOR_TASK, JobID: "job1", ActorID: "actor1", ActorTaskName: "increment", State: eventtypes.RUNNING}

	tests := []struct {
		scenario            string
		tasks               []eventtypes.Task
		opts                ListAPIOptions
		expectedKeys        []string
		expectedNumFiltered int
	}{
		{
			scenario:            "driver tasks are dropped by default",
			tasks:               []eventtypes.Task{driver, running},
			opts:                ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: true},
			expectedKeys:        []string{"t1#0"},
			expectedNumFiltered: 1,
		},
		{
			scenario:            "the driver task is kept when exclude_driver is off",
			tasks:               []eventtypes.Task{driver, running},
			opts:                ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: false},
			expectedKeys:        []string{"d1#0", "t1#0"},
			expectedNumFiltered: 2,
		},
		{
			scenario: "an equality filter keeps only the matching tasks",
			tasks:    []eventtypes.Task{running, finished, otherJob},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "job_id", FilterPredicate: PredicateEqual, FilterValue: "job1"}},
			},
			expectedKeys:        []string{"t1#0", "t2#0"},
			expectedNumFiltered: 2,
		},
		{
			scenario: "multiple filters are ANDed together",
			// t2 matches the job but not the state, t3 matches the state but not the job,
			// so only t1 can survive both filters.
			tasks: []eventtypes.Task{running, finished, otherJob},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters: []Filter{
					{FilterKey: "job_id", FilterPredicate: PredicateEqual, FilterValue: "job1"},
					{FilterKey: "state", FilterPredicate: PredicateEqual, FilterValue: string(eventtypes.RUNNING)},
				},
			},
			expectedKeys:        []string{"t1#0"},
			expectedNumFiltered: 1,
		},
		{
			scenario: "a not-equal filter keeps everything else",
			tasks:    []eventtypes.Task{running, finished, otherJob},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "state", FilterPredicate: PredicateNotEqual, FilterValue: string(eventtypes.RUNNING)}},
			},
			expectedKeys:        []string{"t2#0"},
			expectedNumFiltered: 1,
		},
		{
			scenario: "task_name matches an actor task's method name",
			// GetFilterableFieldValue reads ActorTaskName for ACTOR_TASK, so the normal
			// task's own TaskName must not be what gets compared here.
			tasks: []eventtypes.Task{actorMethod, running},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "task_name", FilterPredicate: PredicateEqual, FilterValue: "increment"}},
			},
			expectedKeys:        []string{"t4#0"},
			expectedNumFiltered: 1,
		},
		{
			scenario: "an empty filter value matches tasks whose field is empty",
			// ApplyFilter skips a filter with an empty value, but this path does not:
			// the normal task has no actor ID, so it is the one that matches.
			tasks: []eventtypes.Task{actorMethod, running},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "actor_id", FilterPredicate: PredicateEqual, FilterValue: ""}},
			},
			expectedKeys:        []string{"t1#0"},
			expectedNumFiltered: 1,
		},
		{
			scenario: "an unknown filter key matches nothing",
			tasks:    []eventtypes.Task{running, finished},
			opts: ListAPIOptions{
				Limit:         DefaultLimit,
				ExcludeDriver: true,
				Filters:       []Filter{{FilterKey: "worker_id", FilterPredicate: PredicateEqual, FilterValue: "worker1"}},
			},
			expectedKeys:        []string{},
			expectedNumFiltered: 0,
		},
		{
			scenario: "results are sorted by task ID and then by attempt",
			tasks: []eventtypes.Task{
				{TaskID: "t2", TaskAttempt: 1, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t1", TaskAttempt: 2, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t2", TaskAttempt: 0, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t1", TaskAttempt: 0, TaskType: eventtypes.NORMAL_TASK},
			},
			opts:                ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: true},
			expectedKeys:        []string{"t1#0", "t1#2", "t2#0", "t2#1"},
			expectedNumFiltered: 4,
		},
		{
			scenario: "the limit truncates after sorting, and numFiltered keeps the pre-limit count",
			tasks: []eventtypes.Task{
				{TaskID: "t2", TaskAttempt: 1, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t1", TaskAttempt: 2, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t2", TaskAttempt: 0, TaskType: eventtypes.NORMAL_TASK},
				{TaskID: "t1", TaskAttempt: 0, TaskType: eventtypes.NORMAL_TASK},
			},
			opts:                ListAPIOptions{Limit: 2, ExcludeDriver: true},
			expectedKeys:        []string{"t1#0", "t1#2"},
			expectedNumFiltered: 4,
		},
		{
			scenario: "a zero limit drops every result while still reporting the count",
			// ParseOptionsFromReq only defaults the limit when the parameter is absent,
			// so an explicit ?limit=0 reaches here as well.
			tasks:               []eventtypes.Task{running, finished},
			opts:                ListAPIOptions{Limit: 0, ExcludeDriver: true},
			expectedKeys:        []string{},
			expectedNumFiltered: 2,
		},
		{
			scenario:            "no tasks at all",
			tasks:               []eventtypes.Task{},
			opts:                ListAPIOptions{Limit: DefaultLimit, ExcludeDriver: true},
			expectedKeys:        []string{},
			expectedNumFiltered: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			got, numFiltered := ApplyTaskFilters(tt.tasks, tt.opts)

			// Equal rather than ElementsMatch: ApplyTaskFilters owns the ordering.
			assert.Equal(t, tt.expectedKeys, taskKeys(got))
			assert.Equal(t, tt.expectedNumFiltered, numFiltered)
			assert.NotNil(t, got, "ApplyTaskFilters always returns a slice, even when nothing matches")
		})
	}
}

func TestApplyFilter(t *testing.T) {
	running := eventtypes.Task{TaskID: "t1", TaskType: eventtypes.NORMAL_TASK, JobID: "job1", State: eventtypes.RUNNING}
	finished := eventtypes.Task{TaskID: "t2", TaskType: eventtypes.NORMAL_TASK, JobID: "job2", State: eventtypes.FINISHED}
	items := []eventtypes.Task{running, finished}

	tests := []struct {
		scenario        string
		filterKey       string
		filterPredicate string
		filterValue     string
		expectedKeys    []string
	}{
		{
			scenario:        "an empty key leaves the items untouched",
			filterKey:       "",
			filterPredicate: "=",
			filterValue:     "RUNNING",
			expectedKeys:    []string{"t1#0", "t2#0"},
		},
		{
			scenario:        "an empty value leaves the items untouched",
			filterKey:       "state",
			filterPredicate: "=",
			filterValue:     "",
			expectedKeys:    []string{"t1#0", "t2#0"},
		},
		{
			scenario:        "equality keeps the matching item",
			filterKey:       "state",
			filterPredicate: "=",
			filterValue:     "RUNNING",
			expectedKeys:    []string{"t1#0"},
		},
		{
			scenario:        "inequality keeps everything else",
			filterKey:       "state",
			filterPredicate: "!=",
			filterValue:     "RUNNING",
			expectedKeys:    []string{"t2#0"},
		},
		{
			scenario: "an unsupported predicate falls back to equality",
			// parsePredicate fails here, and the zero PredicateType is not in PredicateMap,
			// so the equality predicate is used rather than dropping or keeping everything.
			filterKey:       "state",
			filterPredicate: ">",
			filterValue:     "RUNNING",
			expectedKeys:    []string{"t1#0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			got := ApplyFilter(items, tt.filterKey, tt.filterPredicate, tt.filterValue, filterableField)

			assert.Equal(t, tt.expectedKeys, taskKeys(got))
		})
	}
}

func TestApplyFilterReturnsNilWhenNothingMatches(t *testing.T) {
	// ApplyTaskFilters returns an empty slice when nothing matches, but ApplyFilter leaves
	// its result nil. Pinning the difference so that future callers are aware of it.
	items := []eventtypes.Task{{TaskID: "t1", TaskType: eventtypes.NORMAL_TASK, State: eventtypes.RUNNING}}

	got := ApplyFilter(items, "state", "=", "FAILED", filterableField)

	assert.Nil(t, got)
}
