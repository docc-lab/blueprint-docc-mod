package socialnetwork

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync" // FIX: needed for the mutex below
	"time"
)

// The UniqueIdService interface
type UniqueIdService interface {
	// Returns a newly generated unique id to be used as a post's unique identifier.
	ComposeUniqueId(ctx context.Context, reqID int64, postType int64) (int64, error)
}

// FIX: the counter part of an ID is now 2 hex digits (up to 256 IDs per
// millisecond) instead of 1. The original kept only the FIRST hex digit of the
// counter, so counter 16 ("10") became "1" and collided with counter 1.
const uniqueIdCounterHexDigits = 2
const uniqueIdMaxCounter = int64(1)<<(4*uniqueIdCounterHexDigits) - 1

// FIX: milliseconds are counted from 2018-01-01 (the custom epoch the original
// DeathStarBench uses), not from 1970. Since 2004, milliseconds-since-1970 need
// 11 hex digits, and the original code kept only the FIRST 10, dropping the
// fastest-changing digit. The timestamp part of an ID then changed only every
// 16 ms while the counter reset every 1 ms, so IDs repeated up to 16 times.
// Measured from 2018, the value fits in 10 hex digits until about 2052.
const uniqueIdCustomEpochMs = int64(1514764800000)

// Implementation of [UserTimelineService]
type UniqueIdServiceImpl struct {
	mu                sync.Mutex // FIX: guards counter and current_timestamp
	counter           int64
	current_timestamp int64
	machine_id        string
}

// Implements UniqueIdService interface
func NewUniqueIdServiceImpl(ctx context.Context) (UniqueIdService, error) {
	return &UniqueIdServiceImpl{counter: 0, current_timestamp: -1, machine_id: GetMachineID()}, nil
}

// FIX: replaces getCounter(timestamp).
//   - The original had no locking. gRPC runs requests concurrently, so several
//     requests read and updated the counter at the same time and got the same ID.
//   - The original read the clock BEFORE taking the counter, so a request holding
//     an older millisecond could reset the counter for a millisecond that was
//     already used and reissue its IDs. The clock is now read inside the lock and
//     never allowed to go backwards.
//   - If all counter values for a millisecond are used, wait for the next
//     millisecond instead of reusing a value.
func (u *UniqueIdServiceImpl) nextTimestampAndCounter() (int64, int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for {
		timestamp := time.Now().UnixNano() / int64(time.Millisecond)
		if timestamp < u.current_timestamp {
			timestamp = u.current_timestamp
		}
		if timestamp != u.current_timestamp {
			u.current_timestamp = timestamp
			u.counter = 1
			return timestamp, 0
		}
		if u.counter <= uniqueIdMaxCounter {
			idx := u.counter
			u.counter += 1
			return timestamp, idx
		}
		time.Sleep(20 * time.Microsecond)
	}
}

// Implements UniqueIdService interface
func (u *UniqueIdServiceImpl) ComposeUniqueId(ctx context.Context, reqID int64, postType int64) (int64, error) {
	timestamp, idx := u.nextTimestampAndCounter()                           // FIX: was time.Now() followed by u.getCounter(timestamp)
	timestamp_hex := strconv.FormatInt(timestamp-uniqueIdCustomEpochMs, 16) // FIX: was FormatInt(timestamp, 16)
	if len(timestamp_hex) > 10 {
		// FIX: keep the LAST 10 digits (the ones that change) if this is ever
		// reached; the original kept the first 10. Not reached before ~2052.
		timestamp_hex = timestamp_hex[len(timestamp_hex)-10:]
	} else if len(timestamp_hex) < 10 {
		timestamp_hex = strings.Repeat("0", 10-len(timestamp_hex)) + timestamp_hex
	}
	// FIX: zero-pad the counter to a fixed width; it can no longer exceed that
	// width, so nothing is ever cut off.
	counter_hex := strconv.FormatInt(idx, 16)
	if len(counter_hex) < uniqueIdCounterHexDigits {
		counter_hex = strings.Repeat("0", uniqueIdCounterHexDigits-len(counter_hex)) + counter_hex
	}
	log.Println(u.machine_id, timestamp_hex, counter_hex)
	post_id_str := u.machine_id + timestamp_hex + counter_hex
	post_id, err := strconv.ParseInt(post_id_str, 16, 64)
	if err != nil {
		return 0, err
	}
	post_id = post_id & 0x7FFFFFFFFFFFFFFF
	return post_id, nil
}