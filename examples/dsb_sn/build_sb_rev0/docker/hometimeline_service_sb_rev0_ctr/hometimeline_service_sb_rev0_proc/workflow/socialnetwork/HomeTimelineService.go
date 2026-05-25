package socialnetwork

import (
	"context"
	"log"
	"strconv"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/redis"
)

// The HomeTimelineService Interface
// The full Timeline of a user is represented as an array of post ids: post_ids[id_0 ,..., id_n].
type HomeTimelineService interface {
	// Reads the timeline of the user that has the id `userID`.
	// The return value is represented by the slice: post_ids[start:stop].
	ReadHomeTimeline(ctx context.Context, reqID int64, userID int64, start int64, stop int64) ([]int64, error)
	// Adds a new post to the home timeline of the following users:
	// (i)   user with id `userID`,
	// (ii)  all the followers of the user with `userID`
	// (iii) all the mentioned users in the post listed in the `userMentionIDs`.
	// The new post ID is placed at the nth position in the post ids array.
	//    post_ids = append(post_ids, `postID`)
	WriteHomeTimeline(ctx context.Context, reqID int64, postID int64, userID int64, timestamp int64, userMentionIDs []int64) error
}

// Implementation of [HomeTimelineService]
type HomeTimelineServiceImpl struct {
	homeTimelineCache  backend.Cache
	postStorageService PostStorageService
	socialGraphService SocialGraphService
}

// Creates a [HomeTimelineService] instance that maintains the home timelines for the various users.
func NewHomeTimelineServiceImpl(ctx context.Context, homeTimelineCache backend.Cache, postStorageService PostStorageService, socialGraphService SocialGraphService) (HomeTimelineService, error) {
	return &HomeTimelineServiceImpl{homeTimelineCache: homeTimelineCache, postStorageService: postStorageService, socialGraphService: socialGraphService}, nil
}

// Implements HomeTimelineService interface
func (h *HomeTimelineServiceImpl) WriteHomeTimeline(ctx context.Context, reqID int64, postID int64, userID int64, timestamp int64, userMentionIDs []int64) error {
	log.Printf("WriteHomeTimeline: START reqID=%d postID=%d userID=%d mentions=%d", reqID, postID, userID, len(userMentionIDs))
	followers, err := h.socialGraphService.GetFollowers(ctx, reqID, userID)
	if err != nil {
		log.Printf("WriteHomeTimeline: GetFollowers FAILED reqID=%d userID=%d error=%v", reqID, userID, err)
		return err
	}
	log.Printf("WriteHomeTimeline: GetFollowers SUCCESS reqID=%d userID=%d followers=%d", reqID, userID, len(followers))
	followers_set := make(map[int64]bool)
	for _, follower := range followers {
		followers_set[follower] = true
	}
	for _, um := range userMentionIDs {
		followers_set[um] = true
	}
	log.Printf("WriteHomeTimeline: Total recipients reqID=%d postID=%d count=%d", reqID, postID, len(followers_set))

	// Try to use Redis sorted sets if available (much more efficient)
	if redisCache, ok := h.homeTimelineCache.(*redis.RedisCache); ok {
		postIDStr := strconv.FormatInt(postID, 10)
		timestampFloat := float64(timestamp)
		// Use Redis pipelining to batch all ZAdd operations into a single network round-trip
		// This is much more efficient than sequential calls or spawning N goroutines
		operations := make([]struct {
			Key    string
			Score  float64
			Member string
		}, 0, len(followers_set))
		for id := range followers_set {
			operations = append(operations, struct {
				Key    string
				Score  float64
				Member string
			}{
				Key:    strconv.FormatInt(id, 10),
				Score:  timestampFloat,
				Member: postIDStr,
			})
		}
		err := redisCache.ZAddBatch(ctx, operations)
		if err != nil {
			log.Printf("WriteHomeTimeline: ZAddBatch FAILED reqID=%d postID=%d recipients=%d error=%v", reqID, postID, len(followers_set), err)
			return err
		}
		return nil
	}
	log.Printf("WriteHomeTimeline: Using fallback (non-Redis) cache reqID=%d postID=%d", reqID, postID)

	// Fallback to old read-modify-write pattern for non-Redis caches
	const maxCachedTimelineSize = 5000
	for id, _ := range followers_set {
		id_str := strconv.FormatInt(id, 10)
		var posts []PostInfo
		_, err = h.homeTimelineCache.Get(ctx, id_str, &posts)
		if err != nil {
			log.Printf("WriteHomeTimeline: Cache Get FAILED reqID=%d postID=%d followerID=%s error=%v", reqID, postID, id_str, err)
			return err
		}
		posts = append(posts, PostInfo{PostID: postID, Timestamp: timestamp})
		// Limit the cached timeline to the most recent posts
		if len(posts) > maxCachedTimelineSize {
			posts = posts[len(posts)-maxCachedTimelineSize:]
		}
		err = h.homeTimelineCache.Put(ctx, id_str, posts)
		if err != nil {
			log.Printf("WriteHomeTimeline: Cache Put FAILED reqID=%d postID=%d followerID=%s error=%v", reqID, postID, id_str, err)
			return err
		}
	}
	log.Printf("WriteHomeTimeline: Fallback cache SUCCESS reqID=%d postID=%d recipients=%d", reqID, postID, len(followers_set))
	return nil
}

// Implements HomeTimelineService interface
func (h *HomeTimelineServiceImpl) ReadHomeTimeline(ctx context.Context, reqID int64, userID int64, start int64, stop int64) ([]int64, error) {
	if stop <= start || start < 0 {
		return []int64{}, nil
	}
	userIDStr := strconv.FormatInt(userID, 10)

	// Try to use Redis sorted sets if available (much more efficient)
	if redisCache, ok := h.homeTimelineCache.(*redis.RedisCache); ok {
		// ZRevRange: get range from sorted set (ordered by score descending)
		// stop-1 because ZRevRange is inclusive on both ends
		postIDStrs, err := redisCache.ZRevRange(ctx, userIDStr, start, stop-1)
		if err != nil {
			return []int64{}, err
		}
		postIDs := make([]int64, 0, len(postIDStrs))
		for _, postIDStr := range postIDStrs {
			postID, err := strconv.ParseInt(postIDStr, 10, 64)
			if err != nil {
				return []int64{}, err
			}
			postIDs = append(postIDs, postID)
		}
		_, err = h.postStorageService.ReadPosts(ctx, reqID, postIDs)
		if err != nil {
			return postIDs, err
		}
		return postIDs, nil
	}

	// Fallback to old method for non-Redis caches
	var postIDs []int64
	var postInfos []PostInfo
	_, err := h.homeTimelineCache.Get(ctx, userIDStr, &postInfos)
	if err != nil {
		return []int64{}, err
	}
	for _, pinfo := range postInfos {
		postIDs = append(postIDs, pinfo.PostID)
	}
	if start < int64(len(postIDs)) {
		minstop := stop
		if stop > int64(len(postIDs)) {
			minstop = int64(len(postIDs))
		}
		postIDs = postIDs[start:minstop]
	}
	_, err = h.postStorageService.ReadPosts(ctx, reqID, postIDs)
	if err != nil {
		return postIDs, err
	}
	return postIDs, nil
}
