package cli

import "time"

var taskSearchSortValues = map[string]struct{}{
	"due_date":     {},
	"created_at":   {},
	"completed_at": {},
	"likes":        {},
	"relevance":    {},
	"modified_at":  {},
}

// parseTaskFilterTimestamp validates timestamps while leaving callers free to
// send the user's original value to Asana unchanged.
func parseTaskFilterTimestamp(flag, value string, allowNow bool) (time.Time, error) {
	if allowNow && value == "now" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		suffix := ""
		if allowNow {
			suffix = " or the literal \"now\""
		}
		return time.Time{}, usageErrorf("%s must be an RFC3339 timestamp with a timezone%s; got %q", flag, suffix, value)
	}
	return parsed, nil
}

func validTaskSearchSort(value string) bool {
	_, ok := taskSearchSortValues[value]
	return ok
}
