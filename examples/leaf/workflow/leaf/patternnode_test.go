package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestPattern(t *testing.T, source string, children map[string]TreeNode) *PatternNodeImpl {
	t.Helper()
	var pattern CallPattern
	if err := json.Unmarshal([]byte(source), &pattern); err != nil {
		t.Fatal(err)
	}
	names, err := pattern.DependencyNames()
	if err != nil {
		t.Fatal(err)
	}
	var dependencies []TreeNode
	for _, name := range names {
		dependencies = append(dependencies, children[name])
	}
	config, err := json.Marshal(PatternConfig{Dependencies: names, Pattern: pattern})
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewPatternNodeImpl(context.Background(), string(config), dependencies...)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestPatternSequenceParallelAndRepeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var before, leftDone, after atomic.Bool
	var rightCalls atomic.Int32
	node := newTestPattern(t, `{"sequence":[{"call":"before"},{"parallel":[{"call":"left"},{"repeat":{"count":3,"do":{"call":"right"}}}],"parallelism":2},{"call":"after"}]}`, map[string]TreeNode{
		"before": treeNodeFunc(func(context.Context, int64) (int64, error) { before.Store(true); return 1, nil }),
		"left": treeNodeFunc(func(childCtx context.Context, n int64) (int64, error) {
			if childCtx != ctx || !before.Load() {
				t.Error("parallel group lost its context or started before the first step")
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			leftDone.Store(true)
			return n, nil
		}),
		"right": treeNodeFunc(func(context.Context, int64) (int64, error) {
			if rightCalls.Add(1) == 1 {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}
			return 2, nil
		}),
		"after": treeNodeFunc(func(context.Context, int64) (int64, error) {
			if !leftDone.Load() || rightCalls.Load() != 3 {
				t.Error("sequence advanced before its parallel branches finished")
			}
			after.Store(true)
			return 4, nil
		}),
	})
	done := make(chan struct{}, 1)
	go func() {
		result, err := node.Process(ctx, 10)
		if err != nil || result != 21 {
			t.Errorf("Process = %d, %v; want 21", result, err)
		}
		done <- struct{}{}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("parallel branches did not overlap")
		}
	}
	if after.Load() {
		t.Fatal("final step started before the join")
	}
	close(release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("pattern did not finish")
	}
}

func TestPatternParallelFailureDrainsBeforeStoppingSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	failure := errors.New("failed child")
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var after atomic.Bool
	node := newTestPattern(t, `{"sequence":[{"parallel":[{"call":"fail"},{"call":"wait"}]},{"call":"after"}]}`, map[string]TreeNode{
		"fail": treeNodeFunc(func(context.Context, int64) (int64, error) { started <- struct{}{}; return 0, failure }),
		"wait": treeNodeFunc(func(context.Context, int64) (int64, error) {
			started <- struct{}{}
			select {
			case <-release:
				return 1, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}),
		"after": treeNodeFunc(func(context.Context, int64) (int64, error) { after.Store(true); return 1, nil }),
	})
	done := make(chan error, 1)
	go func() { _, err := node.Process(ctx, 1); done <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("parallel branches did not start")
		}
	}
	select {
	case <-done:
		t.Fatal("returned while a sibling and its tracing wrapper were still active")
	default:
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, failure) || after.Load() {
			t.Fatalf("sequence did not stop after draining: %v, after=%v", err, after.Load())
		}
	case <-ctx.Done():
		t.Fatal("error did not return after join")
	}
}

func TestPatternTimeoutAndSleepCancellation(t *testing.T) {
	node := newTestPattern(t, `{"sleep":"1h"}`, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, err := node.Process(ctx, 1)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sleep ignored cancellation: %v", err)
	}

	parent, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelParent()
	parentDeadline, _ := parent.Deadline()
	node = newTestPattern(t, `{"call":"slow","timeout":"10ms"}`, map[string]TreeNode{
		"slow": treeNodeFunc(func(ctx context.Context, _ int64) (int64, error) {
			deadline, ok := ctx.Deadline()
			if !ok || !deadline.Before(parentDeadline) {
				t.Error("call did not receive its own shorter deadline")
				return 0, context.DeadlineExceeded
			}
			<-ctx.Done()
			return 0, ctx.Err()
		}),
	})
	_, err = node.Process(parent, 1)
	if !errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		t.Fatalf("per-call timeout did not preserve parent context: call=%v parent=%v", err, parent.Err())
	}
}

func TestPatternEmptyGroupsAndZeroRepeat(t *testing.T) {
	for _, source := range []string{`{"sequence":[]}`, `{"parallel":[]}`, `{"repeat":{"count":0,"do":{"call":"unused"}}}`} {
		node := newTestPattern(t, source, map[string]TreeNode{
			"unused": treeNodeFunc(func(context.Context, int64) (int64, error) { t.Error("zero repeat called a child"); return 1, nil }),
		})
		if result, err := node.Process(context.Background(), 7); result != 0 || err != nil {
			t.Fatalf("%s = %d, %v; want 0", source, result, err)
		}
	}
}

func TestPatternConcurrentRequests(t *testing.T) {
	node := newTestPattern(t, `{"parallel":[{"repeat":{"count":5,"do":{"call":"shared"}}},{"repeat":{"count":7,"do":{"call":"shared"}}}]}`, map[string]TreeNode{
		"shared": treeNodeFunc(func(_ context.Context, n int64) (int64, error) { return n + 1, nil }),
	})
	var wg sync.WaitGroup
	for n := int64(0); n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result, err := node.Process(context.Background(), n); err != nil || result != 12*(n+1) {
				t.Errorf("request %d = %d, %v", n, result, err)
			}
		}()
	}
	wg.Wait()
}

func TestPatternValidationAndDependencyBinding(t *testing.T) {
	for _, source := range []string{
		`{}`, `{"call":"a","sleep":"1ms"}`, `{"parallel":[],"parallelism":-1}`,
		`{"call":"a","parallelism":1}`, `{"sleep":"-1ms"}`, `{"sleep":"bad"}`,
		`{"call":"a","timeout":"0s"}`, `{"call":"a","timeout":"bad"}`, `{"sleep":"1ms","timeout":"1s"}`,
		`{"repeat":{"do":{"call":"a"}}}`, `{"repeat":{"count":-1,"do":{"call":"a"}}}`,
		`{"repeat":{"count":1}}`, `{"sequence":[{}]}`,
	} {
		var pattern CallPattern
		if err := json.Unmarshal([]byte(source), &pattern); err != nil {
			t.Fatal(err)
		}
		if _, err := pattern.DependencyNames(); err == nil {
			t.Errorf("accepted invalid pattern %s", source)
		}
	}
	var pattern CallPattern
	if err := json.Unmarshal([]byte(`{"sequence":[{"call":"b"},{"parallel":[{"call":"a"},{"call":"b"}]}]}`), &pattern); err != nil {
		t.Fatal(err)
	}
	if names, err := pattern.DependencyNames(); err != nil || !reflect.DeepEqual(names, []string{"b", "a"}) {
		t.Fatalf("dependency order/deduplication: %v, %v", names, err)
	}
	child := treeNodeFunc(func(_ context.Context, n int64) (int64, error) { return n, nil })
	for _, config := range []string{
		`{"dependencies":["wrong"],"pattern":{"call":"a"}}`,
		`{"dependencies":["a","a"],"pattern":{"call":"a"}}`,
		`{"dependencies":["a"],"pattern":{"call":"a","typo":1}}`,
		`{"dependencies":["a"],"pattern":{"call":"a"}} {}`,
	} {
		if _, err := NewPatternNodeImpl(context.Background(), config, child); err == nil {
			t.Errorf("accepted invalid binding %s", config)
		}
	}
	config := `{"dependencies":["a"],"pattern":{"call":"a"}}`
	if _, err := NewPatternNodeImpl(context.Background(), config, nil); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("accepted nil dependency: %v", err)
	}
}
