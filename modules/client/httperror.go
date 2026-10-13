package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError is a non-2xx REST or stream response, carrying the status and the
// (possibly empty) response body so an auth failure is distinguishable from a
// transport fault.
type HTTPError struct {
	Body   string
	Status int
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("client: http %d", e.Status)
	}
	return fmt.Sprintf("client: http %d: %s", e.Status, e.Body)
}

// IsUnauthorized reports whether err is a 401/403 from an instance.
func IsUnauthorized(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden
	}
	return false
}
