// Package boundedio reads untrusted response bodies with a fixed upper bound.
//
// Successful HTTP responses from providers, OAuth authorization servers, and
// MCP servers are validated only after they are buffered, so an unbounded read
// lets a malicious or compromised endpoint exhaust the client's memory before
// any check runs. The package turns "read the rest of the body" into "read this
// much or fail", which keeps the failure a normal protocol error.
package boundedio

import (
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge reports a body larger than the caller's limit. Callers surface it
// as the protocol error it is: the peer sent more than this client accepts.
var ErrTooLarge = errors.New("response body exceeds the size limit")

// ReadAll reads r in full, failing with ErrTooLarge when it exceeds limit
// bytes. A non-positive limit means no bound, which is only for callers that
// already know the size.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limit)
	}
	return data, nil
}
