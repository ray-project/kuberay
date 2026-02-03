package utils

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/emicklei/go-restful/v3"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

const (
	// Ref: https://github.com/ray-project/ray/blob/ac76c16ebf081bad2bc9b73e952d855dba3fc021/python/ray/util/state/common.py#L48.
	DefaultLimit = 100

	// Ref: https://github.com/ray-project/ray/blob/ac76c16ebf081bad2bc9b73e952d855dba3fc021/python/ray/util/state/common.py#L52-L61.
	RayMaxLimitFromAPIServer  = 10000
	RayMaxLimitFromDataSource = 10000
)

type PredicateType string

const (
	PredicateEqual    PredicateType = "="
	PredicateNotEqual PredicateType = "!="
)

type Filter struct {
	FilterKey       string
	FilterPredicate PredicateType
	FilterValue     string
}

type ListAPIOptions struct {
	Limit         int
	Timeout       int
	Detail        bool
	ExcludeDriver bool
	Filters       []Filter
}

// ParseOptionsFromReq parses the query parameters from the request and returns the API options for list methods.
// Ref: https://github.com/ray-project/ray/blob/01ac7c99b900a882c3109ba4d99209bef817ceea/python/ray/dashboard/state_api_utils.py#L76-L101.
func ParseOptionsFromReq(req *restful.Request) (ListAPIOptions, error) {
	opts := ListAPIOptions{
		Limit:         DefaultLimit,
		ExcludeDriver: true,
	}

	parse := func(key string, opt interface{}) error {
		val := req.QueryParameter(key)
		if val == "" {
			return nil
		}

		switch t := opt.(type) {
		case *int:
			intVal, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("invalid %s: %w", key, err)
			}
			if intVal < 0 {
				return fmt.Errorf("invalid %s: cannot be negative", key)
			}
			*t = intVal
		case *bool:
			boolVal, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("invalid %s: %w", key, err)
			}
			*t = boolVal
		default:
			return fmt.Errorf("unsupported type %T for %s", t, key)
		}
		return nil
	}

	if err := parse("limit", &opts.Limit); err != nil {
		return opts, err
	}
	if opts.Limit > RayMaxLimitFromAPIServer {
		return opts, fmt.Errorf("limit cannot be greater than %d, please use a lower limit", RayMaxLimitFromAPIServer)
	}

	if err := parse("timeout", &opts.Timeout); err != nil {
		return opts, err
	}
	if err := parse("detail", &opts.Detail); err != nil {
		return opts, err
	}
	if err := parse("exclude_driver", &opts.ExcludeDriver); err != nil {
		return opts, err
	}

	filters, err := getFiltersFromReq(req)
	if err != nil {
		return opts, fmt.Errorf("invalid filters parameter: %w", err)
	}
	opts.Filters = filters

	return opts, nil
}

// getFiltersFromReq constructs a slice of filters, each of which is a filter triple (key, predicate, value).
func getFiltersFromReq(req *restful.Request) ([]Filter, error) {
	filterKeys := req.QueryParameters("filter_keys")
	filterPredicates := req.QueryParameters("filter_predicates")
	filterValues := req.QueryParameters("filter_values")
	if len(filterKeys) != len(filterPredicates) || len(filterKeys) != len(filterValues) {
		return nil, fmt.Errorf("filter_keys, filter_predicates, and filter_values must have the same length")
	}

	if len(filterKeys) == 0 {
		return []Filter{}, nil
	}

	filters := make([]Filter, len(filterKeys))
	for i := range filterKeys {
		// TODO(jwj): Add error handling for invalid filterkeys and predicates.
		filters[i] = Filter{
			FilterKey:       string(filterKeys[i]),
			FilterPredicate: parsePredicate(string(filterPredicates[i])),
			FilterValue:     string(filterValues[i]),
		}
	}

	return filters, nil
}

// parsePredicate converts a string predicate to a PredicateType.
func parsePredicate(predicate string) PredicateType {
	switch predicate {
	case "!=":
		return PredicateNotEqual
	default:
		return PredicateEqual
	}
}

// ApplyFilters applies the filters to the tasks and returns the filtered tasks.
// The filtering process follows the following steps:
// 1. Exclude driver tasks
// 2. Apply field-specific filters, including job_id, task_id, actor_id, task_name, and state
// 3. Sort tasks by task_id and task_attempt
// 4. Limit the number of tasks
//
// NOTE: Excluding fields not shown in non-detailed mode is implemented in formatTaskForResponse right before sending back the resp.
// TODO(jwj): Add Ray counterparts here.
func ApplyTaskFilters(tasks []eventtypes.Task, listAPIOptions ListAPIOptions) ([]eventtypes.Task, int) {
	// Exclude driver tasks.
	if listAPIOptions.ExcludeDriver {
		tasks = applyFilter(tasks, Filter{
			FilterKey:       "task_type",
			FilterPredicate: PredicateNotEqual,
			FilterValue:     string(eventtypes.DRIVER_TASK),
		})
	}

	// Apply field-specific filters, including job_id, task_id, actor_id, task_name, and state.
	for _, filter := range listAPIOptions.Filters {
		tasks = applyFilter(tasks, filter)
	}

	numFiltered := len(tasks)
	if numFiltered == 0 {
		return []eventtypes.Task{}, 0
	}

	// Sort tasks by task_id and task_attempt.
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID || (tasks[i].TaskID == tasks[j].TaskID && tasks[i].TaskAttempt < tasks[j].TaskAttempt)
	})

	// Limit the number of tasks.
	if len(tasks) > listAPIOptions.Limit {
		tasks = tasks[:listAPIOptions.Limit]
	}

	return tasks, numFiltered
}

// applyFilter applies a filter to the tasks and returns the filtered tasks.
func applyFilter(tasks []eventtypes.Task, filter Filter) []eventtypes.Task {
	filteredTasks := make([]eventtypes.Task, 0)
	for _, task := range tasks {
		fieldValue := task.GetFilterableFieldValue(filter.FilterKey)
		switch filter.FilterPredicate {
		case PredicateEqual:
			if fieldValue == filter.FilterValue {
				filteredTasks = append(filteredTasks, task)
			}
		case PredicateNotEqual:
			if fieldValue != filter.FilterValue {
				filteredTasks = append(filteredTasks, task)
			}
		}
	}

	return filteredTasks
}

// TODO(jwj): ApplyFilters and helpers like sortByIdAndAttempt and limitByLimit should be shared among different objects, e.g., actors, nodes.
// The following functions are for compatibility for other endpoints other than tasks.
type PredicateFunc func(fieldValue, filterValue string) bool

var PredicateMap = map[PredicateType]PredicateFunc{
	PredicateEqual:    func(field, value string) bool { return field == value },
	PredicateNotEqual: func(field, value string) bool { return field != value },
}

func ApplyFilter[T any](items []T, filterKey, filterPredicate, filterValue string, fieldGetter func(T, string) string) []T {
	if filterKey == "" || filterValue == "" {
		return items
	}

	predicate := parsePredicate(filterPredicate)
	predicateFunc, ok := PredicateMap[predicate]
	if !ok {
		predicateFunc = PredicateMap[PredicateEqual]
	}

	var result []T
	for _, item := range items {
		fieldValue := fieldGetter(item, filterKey)
		if predicateFunc(fieldValue, filterValue) {
			result = append(result, item)
		}
	}
	return result
}
