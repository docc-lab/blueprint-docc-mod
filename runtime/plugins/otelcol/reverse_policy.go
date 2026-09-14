package otelcol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

var errInvalidReversePolicy = errors.New("invalid reverse checkpoint policy")

const (
	reversePolicyTTL          = "ttl"
	reversePolicyProbability  = "probability"
	reversePolicyInverseDepth = "inverse_depth"
	reversePolicyDepthLinear  = "depth_linear"
)

// Tomislav-RetCtx: collector-discovered reverse routing, immutable before spans
// start. Zero value keeps TTL behavior; probability policies need no new carrier
// metadata because every truss already contains its leaf's absolute depth.
type reversePolicy struct {
	mode        string
	probability float64
}

func parseReversePolicy(config map[string]interface{}) (reversePolicy, error) {
	policy := reversePolicy{mode: reversePolicyTTL}
	if value, present := config["reverse_policy"]; present {
		mode, ok := value.(string)
		if !ok {
			return reversePolicy{}, fmt.Errorf("%w: reverse_policy must be a string", errInvalidReversePolicy)
		}
		policy.mode = mode
	}
	value, hasProbability := config["reverse_probability"]
	switch policy.mode {
	case reversePolicyTTL, reversePolicyInverseDepth, reversePolicyDepthLinear:
		if hasProbability {
			return reversePolicy{}, fmt.Errorf("%w: reverse_probability requires reverse_policy: probability", errInvalidReversePolicy)
		}
	case reversePolicyProbability:
		probability, ok := reverseProbabilityValue(value)
		if !hasProbability || !ok {
			return reversePolicy{}, fmt.Errorf("%w: probability policy requires finite reverse_probability in [0,1]", errInvalidReversePolicy)
		}
		policy.probability = probability
	default:
		return reversePolicy{}, fmt.Errorf("%w: reverse_policy must be ttl, probability, inverse_depth, or depth_linear", errInvalidReversePolicy)
	}
	return policy, nil
}

func reverseProbabilityValue(value interface{}) (float64, bool) {
	var probability float64
	var err error
	switch v := value.(type) {
	case float64:
		probability = v
	case int:
		probability = float64(v)
	case int64:
		probability = float64(v)
	case json.Number:
		probability, err = v.Float64()
	case string:
		probability, err = strconv.ParseFloat(v, 64)
	default:
		return 0, false
	}
	return probability, err == nil && !math.IsNaN(probability) && probability >= 0 && probability <= 1
}

func (p reversePolicy) probabilistic() bool {
	return p.mode != "" && p.mode != reversePolicyTTL
}

// Tomislav-RetCtx: n is this truss's leaf depth, not the deepest sibling's depth;
// d is the current receiver depth. Linear probabilities sum to one across the
// n potential receivers (d=0..n-1), just like 1/n, but favor deeper receivers.
// These are conditional per-node probabilities, not final emission weights.
func (p reversePolicy) probabilityAt(d, n uint64) float64 {
	if n == 0 || d >= n {
		return 0 // Invalid ancestry cannot supply a depth-based decision.
	}
	switch p.mode {
	case reversePolicyProbability:
		return p.probability
	case reversePolicyInverseDepth:
		return 1 / float64(n)
	case reversePolicyDepthLinear:
		// Convert before adding/multiplying to avoid uint64 overflow.
		return (2 * ((float64(d) + 1) / float64(n))) / (float64(n) + 1)
	default:
		return 0
	}
}

func (p reversePolicy) route(retCtx string, originalCheckpoint bool, depth uint64, draw func() float64) (string, string) {
	if !p.probabilistic() {
		return backend.RouteRetCtx(retCtx, originalCheckpoint)
	}
	return backend.RouteRetCtxWithDecision(retCtx, originalCheckpoint, func(cp backend.ReturnedCheckpoint) bool {
		probability := p.probabilityAt(depth, cp.Depth)
		return probability >= 1 || (probability > 0 && draw() < probability)
	})
}
