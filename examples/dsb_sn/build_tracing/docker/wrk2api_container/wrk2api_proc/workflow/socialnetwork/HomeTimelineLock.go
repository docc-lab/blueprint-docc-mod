package socialnetwork

// FIX (5): added file. WriteHomeTimeline used to update each follower's cached
// home timeline with "read the cached list, append, write it back" and no
// coordination. When two users who share a follower post at the same time,
// both requests read that follower's timeline, each appends its own post, and
// the second write erases the first. The home timeline lives only in the cache,
// so the lost entry is gone and no error is reported. Holding a lock per
// timeline owner makes those updates take turns; different followers'
// timelines are still updated in parallel. Like FIX (4) in
// UserTimelineService.go, this assumes a single replica of this service.

import (
	"context"
	"strconv"
	"sync"
)

// FIX (6): maximum number of entries kept in each cached home timeline.
// Every update reads, decodes, re-encodes and rewrites the whole list, so
// without a limit the cost of each post grows with every post before it (and
// lists eventually exceed memcached's 1 MB item limit). Keeping only the newest
// homeTimelineCap entries makes the cost per post bounded and steady.
const homeTimelineCap = 100

// homeTimelineLocks maps a timeline owner's user ID to a *sync.Mutex.
var homeTimelineLocks sync.Map

func lockHomeTimeline(userID int64) func() {
	m, _ := homeTimelineLocks.LoadOrStore(userID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// appendToHomeTimeline is the body of the loop in WriteHomeTimeline, unchanged
// except that it runs while holding the owner's lock.
func (h *HomeTimelineServiceImpl) appendToHomeTimeline(ctx context.Context, id int64, postID int64, timestamp int64) error {
	defer lockHomeTimeline(id)()
	id_str := strconv.FormatInt(id, 10)
	var posts []PostInfo
	if _, err := h.homeTimelineCache.Get(ctx, id_str, &posts); err != nil {
		return err
	}
	posts = append(posts, PostInfo{PostID: postID, Timestamp: timestamp})
	if len(posts) > homeTimelineCap { // FIX (6): keep only the newest entries (appended at the end)
		posts = posts[len(posts)-homeTimelineCap:]
	}
	return h.homeTimelineCache.Put(ctx, id_str, posts)
}