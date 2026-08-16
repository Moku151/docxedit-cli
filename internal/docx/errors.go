package docx

import "fmt"

// ValidationError identifies a broken package part and, when available, an
// XML source position.
type ValidationError struct {
	Part    string
	Line    int
	Column  int
	Message string
}

func (e *ValidationError) Error() string {
	where := e.Part
	if where == "" {
		where = "package"
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", where, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("%s: %s", where, e.Message)
}

func validationError(part, message string) error {
	return &ValidationError{Part: part, Message: message}
}
