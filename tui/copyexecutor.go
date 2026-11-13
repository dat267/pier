package tui

import "sync"

// copyExecutor runs clipboard work off the UI goroutine: one task at a time, in
// submission order, with the worker exiting when the queue drains.
//
// An offloop queue is not usable here: its worker waits for Stop, and a screen
// can be dropped without Stop (a test that copies and never stops, or a
// replacement screen), which would leave that goroutine running. Ordering is
// the point: two copies triggered back to back must reach the clipboard in that
// order, so the last selection a user made is the one left there.
type copyExecutor struct {
	mu      sync.Mutex
	cond    *sync.Cond
	tasks   []func()
	running bool
}

func newCopyExecutor() *copyExecutor {
	executor := &copyExecutor{}
	executor.cond = sync.NewCond(&executor.mu)
	return executor
}

// Go submits a task. It never blocks and never runs the task under the lock.
func (e *copyExecutor) Go(task func()) {
	e.mu.Lock()
	e.tasks = append(e.tasks, task)
	if !e.running {
		e.running = true
		go e.loop()
	}
	e.mu.Unlock()
}

func (e *copyExecutor) loop() {
	for {
		e.mu.Lock()
		if len(e.tasks) == 0 {
			e.running = false
			e.cond.Broadcast()
			e.mu.Unlock()
			return
		}
		task := e.tasks[0]
		e.tasks[0] = nil
		e.tasks = e.tasks[1:]
		if len(e.tasks) == 0 {
			e.tasks = nil
		}
		e.mu.Unlock()
		task()
	}
}

// Flush waits until every submitted task has finished.
func (e *copyExecutor) Flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for e.running {
		e.cond.Wait()
	}
}
