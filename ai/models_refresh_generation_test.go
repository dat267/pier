package ai

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pi f9a26365 models.ts refresh finally removes its controller only if it is
// still the controller registered for that provider. An old completion cannot
// erase the cancellation handle of the replacement refresh.
func TestOldRefreshCompletionKeepsReplacementCancellation(t *testing.T) {
	firstStarted := make(chan struct{})
	firstMayReturn := make(chan struct{})
	secondStarted := make(chan struct{})
	secondCanceled := make(chan struct{})
	firstDone := make(chan struct{})
	var secondDone chan struct{}
	var calls atomic.Int32
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(firstMayReturn) }) }
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		releaseFirst()
		cancel()
		<-firstDone
		if secondDone != nil {
			<-secondDone
		}
	}()
	models := CreateModels(nil)
	models.SetProvider(&Provider{ID: "p", RefreshModels: func(options *RefreshModelsContext) error {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-options.Ctx.Done()
			<-firstMayReturn
			return nil
		}
		close(secondStarted)
		<-options.Ctx.Done()
		close(secondCanceled)
		return nil
	}})
	go func() {
		models.Refresh(&ModelsRefreshOptions{Ctx: ctx, AllowNetwork: new(false)})
		close(firstDone)
	}()
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first refresh did not start")
	}
	secondDone = make(chan struct{})
	go func() {
		models.Refresh(&ModelsRefreshOptions{Ctx: ctx, AllowNetwork: new(false)})
		close(secondDone)
	}()
	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement refresh did not start")
	}
	releaseFirst()
	<-firstDone // Retired cleanup now runs after the replacement was registered.
	models.DeleteProvider("p")
	select {
	case <-secondCanceled:
	case <-time.After(time.Second):
		t.Fatal("retired refresh erased the replacement's cancellation handle")
	}
}
