package leaf

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type treeNodeFunc func(context.Context, int64) (int64, error)

func (f treeNodeFunc) Process(ctx context.Context, n int64) (int64, error) { return f(ctx, n) }

func newTestFanout(t *testing.T, parallelism string, children ...TreeNode) *FanoutNodeImpl {
	t.Helper()
	node, err := NewFanoutNodeImpl(context.Background(), parallelism, children...)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestFanoutArbitraryWidth(t *testing.T) {
	for _, parallelism := range []string{"0", "1", "3", "100"} {
		for _, width := range []int{0, 1, 2, 9, 65} {
			t.Run(fmt.Sprintf("parallelism=%s/children=%d", parallelism, width), func(t *testing.T) {
				var children []TreeNode
				for i := 0; i < width; i++ {
					children = append(children, treeNodeFunc(func(_ context.Context, n int64) (int64, error) { return n + 1, nil }))
				}
				node := newTestFanout(t, parallelism, children...)
				got, err := node.Process(context.Background(), 7)
				want := int64(width) * 8
				if width == 0 {
					want = 8
				}
				if err != nil || got != want {
					t.Fatalf("Process = %d, %v; want %d", got, err, want)
				}
			})
		}
	}
}

func TestFanoutSequentialOrder(t *testing.T) {
	var order []int
	var children []TreeNode
	for i := 0; i < 6; i++ {
		children = append(children, treeNodeFunc(func(_ context.Context, n int64) (int64, error) {
			order = append(order, i)
			return n, nil
		}))
	}
	_, err := newTestFanout(t, "1", children...).Process(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("order=%v err=%v", order, err)
	}
}

func TestFanoutConcurrentCallsAndErrorDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const width = 8
	started := make(chan int, width)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	firstErr, lastErr := errors.New("first child failed"), errors.New("last child failed")
	var children []TreeNode
	for i := 0; i < width; i++ {
		children = append(children, treeNodeFunc(func(childCtx context.Context, n int64) (int64, error) {
			if childCtx != ctx {
				t.Error("child lost the parent context")
			}
			started <- i
			if i == 0 {
				return 0, firstErr
			}
			select {
			case <-release:
			case <-childCtx.Done():
				return 0, childCtx.Err()
			}
			if i == width-1 {
				return 0, lastErr
			}
			return n, nil
		}))
	}
	done := make(chan error, 1)
	node := newTestFanout(t, "0", children...)
	go func() { _, err := node.Process(ctx, 1); done <- err }()
	// Every child must start while its siblings are still blocked. This proves
	// concurrency without relying on relative execution times.
	for i := 0; i < width; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("children did not start concurrently")
		}
	}
	select {
	case <-done:
		t.Fatal("returned before all child calls and their tracing wrappers finished")
	default:
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, firstErr) || !errors.Is(err, lastErr) {
			t.Fatalf("lost child errors: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("fanout did not finish")
	}
}

func TestFanoutConcurrencyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const width, limit = 9, 3
	started := make(chan struct{}, width)
	release := make(chan struct{})
	var active atomic.Int64
	child := treeNodeFunc(func(ctx context.Context, n int64) (int64, error) {
		if current := active.Add(1); current > limit {
			t.Errorf("active child calls %d exceed limit %d", current, limit)
		}
		defer active.Add(-1)
		started <- struct{}{}
		select {
		case <-release:
			return n, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})
	children := make([]TreeNode, width)
	for i := range children {
		children[i] = child
	}
	node := newTestFanout(t, "3", children...)
	done := make(chan error, 1)
	go func() { _, err := node.Process(ctx, 1); done <- err }()
	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("worker pool did not fill")
		}
	}
	for i := 0; i < width; i++ {
		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal("fanout stalled")
		}
		if i < width-limit {
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("queued child did not start")
			}
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fanout did not finish")
	}
	if active.Load() != 0 {
		t.Fatal("child call outlived its parent")
	}
}

func TestFanoutCancellationDrainsStartedCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 5)
	var calls, active atomic.Int64
	child := treeNodeFunc(func(ctx context.Context, _ int64) (int64, error) {
		calls.Add(1)
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	node := newTestFanout(t, "2", child, child, child, child, child)
	done := make(chan error, 1)
	go func() { _, err := node.Process(ctx, 1); done <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("children did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || active.Load() != 0 || calls.Load() != 2 {
			t.Fatalf("cancellation: err=%v active=%d calls=%d", err, active.Load(), calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not reach children")
	}
}

func TestFanoutConstructorValidationAndIsolation(t *testing.T) {
	for _, value := range []string{"", "-1", "concurrent"} {
		if _, err := NewFanoutNodeImpl(context.Background(), value); err == nil {
			t.Fatalf("accepted invalid parallelism %q", value)
		}
	}
	if _, err := NewFanoutNodeImpl(context.Background(), "0", nil); err == nil {
		t.Fatal("accepted a nil child")
	}
	children := make([]TreeNode, 8)
	for i := range children {
		children[i] = treeNodeFunc(func(_ context.Context, n int64) (int64, error) { return n + 1, nil })
	}
	node := newTestFanout(t, "3", children...)
	children[0] = nil // Caller mutations must not change the constructed node.
	var wg sync.WaitGroup
	for i := int64(0); i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := node.Process(context.Background(), i)
			if err != nil || got != 8*(i+1) {
				t.Errorf("request %d: %d, %v", i, got, err)
			}
		}()
	}
	wg.Wait()
}
