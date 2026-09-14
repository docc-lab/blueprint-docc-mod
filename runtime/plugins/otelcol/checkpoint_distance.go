package otelcol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
)

var errInvalidCheckpointRange = errors.New("invalid checkpoint distance range")

// Tomislav-RetCtx: checkpointRange is immutable after collector config discovery.
// A zero range preserves the legacy fixed-cpd protocol. An explicit range
// enables a one-byte TTL prefix on propagation baggage (not exported trusses).
type checkpointRange struct {
	min, max int
}

func parseCheckpointRange(config map[string]interface{}) (checkpointRange, error) {
	low, hasLow := config["cpd_min"]
	high, hasHigh := config["cpd_max"]
	if !hasLow && !hasHigh {
		return checkpointRange{}, nil
	}
	minCPD, okLow := checkpointBound(low)
	maxCPD, okHigh := checkpointBound(high)
	if !hasLow || !hasHigh || !okLow || !okHigh || minCPD > maxCPD {
		return checkpointRange{}, fmt.Errorf("%w: require integer 1 <= cpd_min <= cpd_max <= 256", errInvalidCheckpointRange)
	}
	return checkpointRange{minCPD, maxCPD}, nil
}

func checkpointBound(value interface{}) (int, bool) {
	var n int64
	switch v := value.(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case float64:
		if math.IsNaN(v) || v < 1 || v > 256 || math.Trunc(v) != v {
			return 0, false
		}
		n = int64(v)
	case json.Number:
		var err error
		n, err = v.Int64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		n, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return int(n), n >= 1 && n <= 256
}

func (r checkpointRange) enabled() bool { return r.min > 0 }

// next runs once per span, at OnStart. A checkpoint chooses one distance for
// all its children; ordinary spans decrement their own copy of the parent TTL.
// The decision is retained by the processor until checkpoint export at OnEnd.
func (r checkpointRange) next(parentTTL byte, hasParent bool) (checkpoint bool, outgoingTTL byte) {
	if hasParent && parentTTL != 0 {
		return false, parentTTL - 1
	}
	return true, r.newTTL()
}

// Tomislav-RetCtx: forward checkpoints and rejected leaves draw independently
// from the same discovered range; neither countdown changes truss geometry.
func (r checkpointRange) newTTL() byte {
	return byte(r.min + rand.IntN(r.max-r.min+1) - 1)
}

func (r checkpointRange) unwrap(raw []byte) (byte, []byte) {
	if !r.enabled() {
		return 0, raw
	}
	if len(raw) < 2 { // A TTL without a bridge payload is not a valid parent.
		return 0, nil
	}
	return raw[0], raw[1:]
}

func (r checkpointRange) wrap(ttl byte, payload []byte) []byte {
	if !r.enabled() {
		return payload
	}
	packed := make([]byte, 1+len(payload))
	packed[0] = ttl
	copy(packed[1:], payload)
	return packed
}
