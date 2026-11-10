package fixture

import (
	"context"
	"sync"
)

type owner struct {
	mu        sync.Mutex
	rw        sync.RWMutex
	listener  func()
	listeners map[int]func()
	cancel    context.CancelFunc
}

var hook func()

func helper() {}

func (o *owner) method() {}

func (o *owner) bad(callback func()) {
	o.mu.Lock()
	o.listener()     // want: field
	callback()       // want: parameter
	o.listeners[1]() // want: map entry
	hook()           // want: package variable
	o.mu.Unlock()
}

func (o *owner) deferred() {
	o.rw.RLock()
	defer o.rw.RUnlock()
	if o.listener != nil {
		o.listener() // want: deferred unlock keeps the lock held
	}
}

func (o *owner) earlyReturn() {
	o.mu.Lock()
	if o.listener == nil {
		o.mu.Unlock()
		return
	}
	o.listener() // want: the nested unlock does not release the outer lock
	o.mu.Unlock()
}

func (o *owner) good(callback func()) {
	o.mu.Lock()
	listener := o.listener
	helper()
	o.method()
	o.cancel()
	go callback()
	defer callback()
	later := func() { callback() }
	o.mu.Unlock()
	listener()
	callback()
	later()
}
