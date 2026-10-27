package coding

import (
	"sync"
	"testing"
)

// TestProcessTreeGuardKillIsConcurrent pins that the guard's kill is safe to
// call from several goroutines at once: the shell tool's abort goroutine and its
// timeout timer can fire together, and both call Kill. The guard must run the
// platform kill once with no unsynchronized state (D178).
func TestProcessTreeGuardKillIsConcurrent(t *testing.T) {
	guard := newProcessTreeGuard(1 << 30) // a pid that cannot exist
	defer guard.Release()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			guard.Kill()
		})
	}
	wg.Wait()
}
