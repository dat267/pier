package offloop

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
)

// D199 separates lossless persistence from optional work. Cancellation must
// reach optional workers before a mandatory queue's drain finishes, and even
// a worker ignoring cancellation must not hold shutdown hostage.
func TestGroupShutdownCancelsOptionalWorkButDrainsSaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup()
		saves := g.Queue()
		optional := g.OptionalQueue()
		releaseSave, releaseOptional := make(chan struct{}), make(chan struct{})
		var saveOnce, optionalOnce sync.Once
		unblockSave := func() { saveOnce.Do(func() { close(releaseSave) }) }
		unblockOptional := func() { optionalOnce.Do(func() { close(releaseOptional) }) }
		defer func() { unblockSave(); unblockOptional(); g.StopAll(); optional.Flush() }()
		var order []int
		saves.Go(func() { <-releaseSave; order = append(order, 1) })
		saves.Go(func() { order = append(order, 2) })
		canceled := make(chan struct{})
		optionalRan := false
		optional.GoContext(func(ctx context.Context) {
			select {
			case <-ctx.Done():
				close(canceled)
				<-releaseOptional
			case <-releaseOptional: // let failed assertions release the worker too
			}
		})
		synctest.Wait()
		optional.Go(func() { optionalRan = true })
		stopped := make(chan struct{})
		go func() { g.StopAll(); close(stopped) }()
		synctest.Wait()
		select {
		case <-canceled:
		default:
			t.Fatal("optional cancellation waited behind a blocked save")
		}
		select {
		case <-stopped:
			t.Fatal("shutdown returned before saves completed")
		default:
		}
		unblockSave()
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("optional worker prevented shutdown after saves drained")
		}
		if !reflect.DeepEqual(order, []int{1, 2}) {
			t.Fatalf("save order = %v, want [1 2]", order)
		}
		optional.Go(func() { optionalRan = true })
		unblockOptional()
		optional.Flush()
		if optionalRan {
			t.Fatal("canceled or late optional task ran after shutdown")
		}
		queued, running := optional.Backlog()
		if queued != 0 || running != 0 {
			t.Fatalf("optional backlog = (%d,%d), want zero", queued, running)
		}
	})
}
