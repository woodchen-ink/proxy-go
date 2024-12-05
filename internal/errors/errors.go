package errors

type ErrorCode int

const (
	ErrInvalidConfig ErrorCode = iota + 1
	ErrRateLimit
	ErrMetricsCollection
)

type MetricsError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *MetricsError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}
