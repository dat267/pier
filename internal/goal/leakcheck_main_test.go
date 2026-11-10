package goal

import (
	"os"
	"testing"

	"github.com/dat267/pier/internal/leakcheck"
)

// TestMain fails the package when a test leaves goroutines running.
func TestMain(m *testing.M) { os.Exit(leakcheck.Run(m)) }
