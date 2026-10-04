package chord

import (
	"context"
	"errors"
)

// Port of chord/context (pi/packages/chord/src/context/index.ts). The port's
// Context is Go's context.Context, so the upstream helpers map onto it: values
// use typed keys, cancellation uses context.CancelFunc, and
// awaitWithContext is a select over ctx.Done().

// BackgroundContext is the empty root context (upstream BACKGROUND_CONTEXT).
var BackgroundContext = context.Background()

// ContextKey is a typed context key (upstream ContextKey).
type ContextKey[T any] struct {
	name string
}

// NewContextKey creates a typed key with a description for diagnostics.
func NewContextKey[T any](description string) ContextKey[T] {
	return ContextKey[T]{name: description}
}

// WithContextValue derives a context containing one additional or replaced
// value (upstream withContextValue).
func WithContextValue[T any](ctx context.Context, key ContextKey[T], value T) context.Context {
	return context.WithValue(ctx, key, value)
}

// ContextValue reads a typed context value.
func ContextValue[T any](ctx context.Context, key ContextKey[T]) (T, bool) {
	value, ok := ctx.Value(key).(T)
	return value, ok
}

// WithCancel derives an independently cancellable child (upstream withCancel).
func WithCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}

// WithoutAbortSignal derives a context retaining all values except caller
// cancellation, for mandatory cleanup (upstream withoutAbortSignal).
func WithoutAbortSignal(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// AwaitWithContext observes work until it settles or ctx is cancelled.
// Cancellation aborts only this waiter; it does not cancel the work
// (upstream awaitWithContext).
func AwaitWithContext[T any](ctx context.Context, work func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := work()
		done <- result{value, err}
	}()
	select {
	case outcome := <-done:
		return outcome.value, outcome.err
	case <-ctx.Done():
		var zero T
		if err := context.Cause(ctx); err != nil {
			return zero, err
		}
		return zero, errors.New("The operation was aborted")
	}
}
