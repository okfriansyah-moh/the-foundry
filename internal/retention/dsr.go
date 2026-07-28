package retention

import "time"

type DSRKind string

const (
	DSRAccess DSRKind = "access"
	DSRExport DSRKind = "export"
	DSRDelete DSRKind = "delete"
)

type DSRRequest struct {
	ID          string
	Principal   string
	Kind        DSRKind
	RequestedAt time.Time
}

type ExportBundle struct {
	Principal   string
	Data        map[string][]string
	GeneratedAt time.Time
}

func BuildExport(principal string, data map[string][]string, now time.Time) ExportBundle {
	copyData := make(map[string][]string, len(data))
	for key, values := range data {
		copyValues := make([]string, len(values))
		copy(copyValues, values)
		copyData[key] = copyValues
	}
	return ExportBundle{Principal: principal, Data: copyData, GeneratedAt: now.UTC()}
}
