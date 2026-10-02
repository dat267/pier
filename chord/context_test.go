package chord

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Port of the context cases of upstream packages/chord/test/context.test.ts.

func TestContextValues(t *testing.T) {
	type greeting string
	key := NewContextKey[greeting]("greeting")
	other := NewContextKey[greeting]("other")
	ctx := WithContextValue(BackgroundContext, key, greeting("hi"))
	if value, ok := ContextValue(ctx, key); !ok || value != "hi" {
		t.Fatalf("value = %q ok=%v", value, ok)
	}
	if _, ok := ContextValue(ctx, other); ok {
		t.Fatal("a different key of the same type must not resolve")
	}
	// A derived value replaces the parent's for the same key.
	nested := WithContextValue(ctx, key, greeting("yo"))
	if value, _ := ContextValue(nested, key); value != "yo" {
		t.Fatalf("nested value = %q", value)
	}
	if value, _ := ContextValue(ctx, key); value != "hi" {
		t.Fatalf("parent changed: %q", value)
	}
}

func TestAwaitWithContextCancellation(t *testing.T) {
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(BackgroundContext)
	done := make(chan error, 1)
	go func() {
		_, err := AwaitWithContext(ctx, func() (string, error) {
			<-release
			return "done", nil
		})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release the waiter")
	}
	close(release)
}

func TestAwaitWithContextResult(t *testing.T) {
	value, err := AwaitWithContext(BackgroundContext, func() (int, error) {
		return 42, nil
	})
	if err != nil || value != 42 {
		t.Fatalf("value = %d err = %v", value, err)
	}
	wantErr := errors.New("boom")
	if _, err := AwaitWithContext(BackgroundContext, func() (int, error) {
		return 0, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithoutAbortSignalKeepsValues(t *testing.T) {
	type name string
	key := NewContextKey[name]("name")
	ctx, cancel := WithCancel(WithContextValue(BackgroundContext, key, name("keep")))
	cancel()
	detached := WithoutAbortSignal(ctx)
	if err := detached.Err(); err != nil {
		t.Fatalf("detached err = %v", err)
	}
	if value, ok := ContextValue(detached, key); !ok || value != "keep" {
		t.Fatalf("detached value = %q ok=%v", value, ok)
	}
}
