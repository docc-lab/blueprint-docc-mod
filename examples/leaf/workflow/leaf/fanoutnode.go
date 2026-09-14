package leaf

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// FanoutNodeImpl calls an arbitrary number of children, passing the request
// context through so tracing wrappers share the parent's fan-in accumulator.
// Instances are immutable after construction and can serve concurrent requests.
type FanoutNodeImpl struct {
	TreeNode
	children    []TreeNode
	parallelism int
}

// NewFanoutNodeImpl accepts the concurrency limit as a string for Blueprint's
// wiring configuration: "1" is sequential, "0" runs all children concurrently,
// and larger values bound the number of simultaneous child calls per request.
// With no children, the node is a leaf and returns n+1.
func NewFanoutNodeImpl(ctx context.Context, parallelism string, children ...TreeNode) (*FanoutNodeImpl, error) {
	limit, err := strconv.Atoi(parallelism)
	if err != nil || limit < 0 {
		return nil, fmt.Errorf("fanout parallelism must be a nonnegative integer, got %q", parallelism)
	}
	for i, child := range children {
		if child == nil {
			return nil, fmt.Errorf("fanout child %d is nil", i)
		}
	}
	return &FanoutNodeImpl{children: append([]TreeNode(nil), children...), parallelism: limit}, nil
}

// Process calls every child once with n and sums their results. Child failures
// do not cancel siblings: all calls finish and all returned trusses reach fan-in
// before the enclosing server span ends. Errors are joined in child-list order.
// Cancellation stops starting new calls and waits for already-started calls.
func (s *FanoutNodeImpl) Process(ctx context.Context, n int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(s.children) == 0 {
		return n + 1, nil
	}
	workers := s.parallelism
	if workers == 0 || workers > len(s.children) {
		workers = len(s.children)
	}
	results := make([]int64, len(s.children))
	errs := make([]error, len(s.children))
	var next atomic.Uint64
	callChildren := func() {
		for ctx.Err() == nil {
			i := int(next.Add(1) - 1)
			if i >= len(s.children) {
				return
			}
			results[i], errs[i] = s.children[i].Process(ctx, n)
		}
	}
	if workers == 1 {
		callChildren()
	} else {
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				callChildren()
			}()
		}
		wg.Wait()
	}
	var sum int64
	var failures []error
	for i, result := range results {
		sum += result
		if errs[i] != nil {
			failures = append(failures, fmt.Errorf("child %d: %w", i, errs[i]))
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	if len(failures) != 0 {
		return 0, errors.Join(failures...)
	}
	return sum, nil
}
