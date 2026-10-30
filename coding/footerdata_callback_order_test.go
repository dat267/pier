package coding

import (
	"path/filepath"
	"testing"
)

// pi v1.4.0 core/footer-data-provider.ts stores listeners in a Set, whose
// iteration order follows registration order.
func TestFooterDataProviderNotifiesListenersInRegistrationOrder(t *testing.T) {
	root := t.TempDir()
	provider := NewFooterDataProvider(root, FooterDataProviderOptions{DisableWatch: true})
	defer provider.Dispose()

	const listenerCount = 24
	var got []int
	for i := 0; i < listenerCount; i++ {
		index := i
		provider.OnBranchChange(func() { got = append(got, index) })
	}

	provider.SetCwd(filepath.Join(root, "next"))
	if len(got) != listenerCount {
		t.Fatalf("got %d notifications, want %d", len(got), listenerCount)
	}
	for i, index := range got {
		if index != i {
			t.Fatalf("notification order = %v, want registration order 0..%d", got, listenerCount-1)
		}
	}
}
