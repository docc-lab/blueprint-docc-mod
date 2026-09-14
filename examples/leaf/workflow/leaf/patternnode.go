package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// CallPattern is a composition of RPCs and local control flow. Exactly one
// action must be present. Parallelism applies to immediate parallel branches;
// timeout applies to one call, including its instrumentation wrapper.
type CallPattern struct {
	Call        string         `json:"call,omitempty" yaml:"call,omitempty"`
	Sequence    *[]CallPattern `json:"sequence,omitempty" yaml:"sequence,omitempty"`
	Parallel    *[]CallPattern `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	Parallelism *int           `json:"parallelism,omitempty" yaml:"parallelism,omitempty"`
	Repeat      *RepeatPattern `json:"repeat,omitempty" yaml:"repeat,omitempty"`
	Sleep       string         `json:"sleep,omitempty" yaml:"sleep,omitempty"`
	Timeout     string         `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

type RepeatPattern struct {
	Count *int         `json:"count" yaml:"count"`
	Do    *CallPattern `json:"do" yaml:"do"`
}

// DependencyNames validates the pattern and lists each referenced service once,
// in first-use order. Repetition changes execution, not dependency registration.
func (p CallPattern) DependencyNames() ([]string, error) {
	var names []string
	seen := make(map[string]bool)
	var visit func(CallPattern, string) error
	visit = func(p CallPattern, path string) error {
		actions := 0
		for _, present := range []bool{p.Call != "", p.Sequence != nil, p.Parallel != nil, p.Repeat != nil, p.Sleep != ""} {
			if present {
				actions++
			}
		}
		if actions != 1 {
			return fmt.Errorf("%s must contain exactly one of call, sequence, parallel, repeat or sleep", path)
		}
		if p.Parallelism != nil && (p.Parallel == nil || *p.Parallelism < 0) {
			return fmt.Errorf("%s.parallelism requires a parallel group and a nonnegative limit", path)
		}
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if p.Call == "" || err != nil || d <= 0 {
				return fmt.Errorf("%s.timeout requires a call and a positive duration", path)
			}
		}
		switch {
		case p.Call != "":
			if !seen[p.Call] {
				seen[p.Call] = true
				names = append(names, p.Call)
			}
		case p.Sleep != "":
			if d, err := time.ParseDuration(p.Sleep); err != nil || d < 0 {
				return fmt.Errorf("%s.sleep must be a nonnegative duration", path)
			}
		case p.Repeat != nil:
			if p.Repeat.Count == nil || *p.Repeat.Count < 0 || p.Repeat.Do == nil {
				return fmt.Errorf("%s.repeat requires a nonnegative count and a do pattern", path)
			}
			return visit(*p.Repeat.Do, path+".repeat.do")
		default:
			steps, action := p.Sequence, "sequence"
			if p.Parallel != nil {
				steps, action = p.Parallel, "parallel"
			}
			for i, step := range *steps {
				if err := visit(step, fmt.Sprintf("%s.%s[%d]", path, action, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(p, "pattern"); err != nil {
		return nil, err
	}
	return names, nil
}

// PatternConfig binds service names used by a pattern to constructor arguments.
// Wiring serializes it as JSON so generated services need no topology file.
type PatternConfig struct {
	Dependencies []string    `json:"dependencies"`
	Pattern      CallPattern `json:"pattern"`
}

type PatternNodeImpl struct {
	TreeNode
	operation TreeNode
}

func NewPatternNodeImpl(ctx context.Context, config string, children ...TreeNode) (*PatternNodeImpl, error) {
	var plan PatternConfig
	decoder := json.NewDecoder(strings.NewReader(config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return nil, fmt.Errorf("decode call pattern: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("call pattern must contain one JSON object")
	}
	names, err := plan.Pattern.DependencyNames()
	if err != nil {
		return nil, err
	}
	if len(plan.Dependencies) != len(children) || len(names) != len(children) {
		return nil, fmt.Errorf("call pattern dependencies do not match its child services")
	}
	dependencies := make(map[string]TreeNode, len(children))
	for i, name := range plan.Dependencies {
		if children[i] == nil || dependencies[name] != nil {
			return nil, fmt.Errorf("nil or duplicate call pattern dependency %q", name)
		}
		dependencies[name] = children[i]
	}
	for _, name := range names {
		if dependencies[name] == nil {
			return nil, fmt.Errorf("missing call pattern dependency %q", name)
		}
	}
	return &PatternNodeImpl{operation: compilePattern(ctx, plan.Pattern, dependencies)}, nil
}

func (s *PatternNodeImpl) Process(ctx context.Context, n int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.operation.Process(ctx, n)
}

type patternOperation struct {
	run func(context.Context, int64) (int64, error)
}

func newPatternOperation(run func(context.Context, int64) (int64, error)) TreeNode {
	return &patternOperation{run: run}
}

func (op patternOperation) Process(ctx context.Context, n int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return op.run(ctx, n)
}

// compilePattern runs once after validation. All mutable execution state,
// including timers and fan-out worker pools, belongs to an individual request.
func compilePattern(ctx context.Context, p CallPattern, dependencies map[string]TreeNode) TreeNode {
	switch {
	case p.Call != "":
		child := dependencies[p.Call]
		timeout, _ := time.ParseDuration(p.Timeout)
		return newPatternOperation(func(ctx context.Context, n int64) (int64, error) {
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			result, err := child.Process(ctx, n)
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				return 0, fmt.Errorf("call %s: %w", p.Call, err)
			}
			return result, nil
		})
	case p.Sleep != "":
		duration, _ := time.ParseDuration(p.Sleep)
		return newPatternOperation(func(ctx context.Context, _ int64) (int64, error) {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
				return 0, ctx.Err()
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		})
	case p.Repeat != nil:
		op, count := compilePattern(ctx, *p.Repeat.Do, dependencies), *p.Repeat.Count
		return newPatternOperation(func(ctx context.Context, n int64) (int64, error) {
			var sum int64
			for i := 0; i < count; i++ {
				value, err := op.Process(ctx, n)
				if err != nil {
					return 0, fmt.Errorf("repeat iteration %d: %w", i, err)
				}
				sum += value
			}
			return sum, nil
		})
	default:
		steps := p.Sequence
		if p.Parallel != nil {
			steps = p.Parallel
		}
		operations := make([]TreeNode, len(*steps))
		for i, step := range *steps {
			operations[i] = compilePattern(ctx, step, dependencies)
		}
		if p.Parallel != nil && len(operations) > 0 {
			limit := 0
			if p.Parallelism != nil {
				limit = *p.Parallelism
			}
			fanout, _ := NewFanoutNodeImpl(ctx, strconv.Itoa(limit), operations...)
			return fanout
		}
		return newPatternOperation(func(ctx context.Context, n int64) (int64, error) {
			var sum int64
			for i, op := range operations {
				value, err := op.Process(ctx, n)
				if err != nil {
					return 0, fmt.Errorf("sequence step %d: %w", i, err)
				}
				sum += value
			}
			return sum, nil
		})
	}
}
