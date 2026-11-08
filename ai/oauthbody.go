package ai

import (
	"fmt"
	"io"

	"github.com/dat267/pier/internal/boundedio"
)

// oauthDocumentBodyLimit bounds one OAuth document this package reads from an
// authorization or token endpoint (D341).
const oauthDocumentBodyLimit = 1 << 20

// readCappedDocument reads a response body of at most limit bytes and reports
// both the bytes read and a protocol error. D341 requires a successful remote
// JSON document to fail at its cap instead of being read as a silent prefix,
// and an interrupted read must not look like a short document (A427). The bytes
// stay available so an error response can still be reported from the part of
// its body that arrived.
func readCappedDocument(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(data)) > limit {
		return data[:limit], fmt.Errorf("%w: more than %d bytes", boundedio.ErrTooLarge, limit)
	}
	return data, err
}
