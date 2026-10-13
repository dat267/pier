package durable

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/sgtdi/fswatcher"
)

type nativeWatch struct {
	watcher fswatcher.Watcher
	cancel  context.CancelFunc
	events  <-chan fswatcher.WatchEvent
	dropped <-chan fswatcher.WatchEvent
	done    <-chan struct{}
}

func startNativeWatch(ctx context.Context, targets []resolvedWatchTarget) (*nativeWatch, error) {
	if runtime.GOOS == "windows" || runtime.GOOS == "android" {
		return nil, nil
	}
	depths := make(map[string]fswatcher.WatchDepth)
	for _, target := range targets {
		path := target.path
		depth := fswatcher.WatchDepth(fswatcher.WatchTopLevel)
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			if target.recursive {
				depth = fswatcher.WatchNested
			}
		} else if err == nil {
			path = filepath.Dir(path)
		} else {
			depth = fswatcher.WatchNested
			for {
				path = filepath.Dir(path)
				info, err = os.Stat(path)
				if err == nil && info.IsDir() {
					break
				}
				if filepath.Dir(path) == path {
					return nil, nil
				}
			}
		}
		if previous, ok := depths[path]; !ok || depth == fswatcher.WatchNested {
			depths[path] = depth
		} else {
			depths[path] = previous
		}
	}
	if len(depths) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(depths))
	for path := range depths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	ready := make(chan struct{})
	options := []fswatcher.WatcherOpt{
		fswatcher.WithCooldown(50 * time.Millisecond),
		fswatcher.WithSeverity(fswatcher.SeverityNone),
		fswatcher.WithReadyChannel(ready),
		fswatcher.WithLogFile(""),
	}
	for _, path := range paths {
		options = append(options, fswatcher.WithPath(path, fswatcher.WithDepth(depths[path])))
	}
	watcher, err := fswatcher.New(options...)
	if err != nil {
		return nil, err
	}
	watchContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	watchErr := make(chan error, 1)
	go func() {
		defer close(done)
		if err := watcher.Watch(watchContext); err != nil {
			watchErr <- err
		}
	}()
	select {
	case <-ready:
		return &nativeWatch{watcher: watcher, cancel: cancel, events: watcher.Events(), dropped: watcher.Dropped(), done: done}, nil
	case err := <-watchErr:
		cancel()
		watcher.Close()
		<-done
		return nil, err
	case <-ctx.Done():
		cancel()
		watcher.Close()
		<-done
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		cancel()
		watcher.Close()
		<-done
		return nil, context.DeadlineExceeded
	}
}

func (n *nativeWatch) close() {
	if n == nil {
		return
	}
	n.cancel()
	n.watcher.Close()
	<-n.done
}
