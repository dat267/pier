package ai

import (
	"errors"
	"testing"
	"testing/synctest"
)

// pi v1.4.0 (4a080252), models.ts createProvider's refreshModels awaits
// fetchModels and propagates its failure. The retained Go image-provider
// wrapper coalesces refresh calls: every waiter must observe that failure.
func TestImagesProviderSharesRefreshFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		failure := errors.New("catalog unavailable")
		calls := 0
		provider := CreateImagesProvider(ImageProviderOptions{
			ID: "test-images",
			RefreshModels: func() ([]*ImagesModel, error) {
				calls++
				close(started)
				<-release
				return nil, failure
			},
			API: OpenRouterImages{},
		})
		results := make(chan error, 2)
		go func() { results <- provider.RefreshModels() }()
		<-started
		go func() { results <- provider.RefreshModels() }()
		synctest.Wait()
		close(release)
		for range 2 {
			if err := <-results; !errors.Is(err, failure) {
				t.Errorf("shared refresh error = %v; want %v", err, failure)
			}
		}
		if calls != 1 {
			t.Errorf("shared refresh fetched %d times; want 1", calls)
		}
	})
}
