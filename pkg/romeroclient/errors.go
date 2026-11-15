package romeroclient

import "fmt"

// APIError represents an HTTP error response returned by the ROMERO REST API.
type APIError struct {
	StatusCode int
	Title      string
	Detail     string
	RawBody    string
}

// Error formats the APIError with HTTP status and detail message.
func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("romeroclient: http %d (%s): %s", e.StatusCode, e.Title, e.Detail)
	}
	if e.Title != "" {
		return fmt.Sprintf("romeroclient: http %d (%s)", e.StatusCode, e.Title)
	}
	return fmt.Sprintf("romeroclient: http %d", e.StatusCode)
}
