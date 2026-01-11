package utils

type PredicateType string

const (
	PredicateEqual    PredicateType = "="
	PredicateNotEqual PredicateType = "!="
)

type PredicateFunc func(fieldValue, filterValue string) bool

var PredicateMap = map[PredicateType]PredicateFunc{
	PredicateEqual:    func(field, value string) bool { return field == value },
	PredicateNotEqual: func(field, value string) bool { return field != value },
}

func ParsePredicate(predicate string) PredicateType {
	switch predicate {
	case "!=":
		return PredicateNotEqual
	default:
		return PredicateEqual
	}
}

func ApplyFilter[T any](items []T, filterKey, filterPredicate, filterValue string, fieldGetter func(T, string) string) []T {
	if filterKey == "" || filterValue == "" {
		return items
	}

	predicate := ParsePredicate(filterPredicate)
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
