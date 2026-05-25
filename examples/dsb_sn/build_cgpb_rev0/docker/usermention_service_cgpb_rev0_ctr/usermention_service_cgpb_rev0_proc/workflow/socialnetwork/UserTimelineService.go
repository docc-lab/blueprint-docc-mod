package socialnetwork

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/redis"
	"go.mongodb.org/mongo-driver/bson"
)

// The UserTimelineService interface
// The full Timeline of a user is represented as an array of post ids: post_ids[id_0 ,..., id_n].
type UserTimelineService interface {
	// Reads the timeline of the user that has the id `userID`.
	// The return value is represented by the slice: post_ids[start:stop].
	ReadUserTimeline(ctx context.Context, reqID int64, userID int64, start int64, stop int64) ([]int64, error)
	// Adds a new post to the user timeline of the user that has the id `userID`
	// The new post ID is placed at the 0th position in the post ids array.
	//    post_ids = []int64{`postID`, post_ids...)
	WriteUserTimeline(ctx context.Context, reqID int64, postID int64, userID int64, timestamp int64) error
}

// The format of a single post in a user's timeline stored in the backend.
type PostInfo struct {
	PostID    int64
	Timestamp int64
}

// The format of a user's timeline stored in the backend.
type UserPosts struct {
	UserID int64
	Posts  []PostInfo
}

// Implementation of [UserTimelineService]
type UserTimelineServiceImpl struct {
	userTimelineCache  backend.Cache
	userTimelineDB     backend.NoSQLDatabase
	postStorageService PostStorageService
	CacheHits          int64
	CacheMiss          int64
	NumRequests        int64
}

// Creates a [UserTimelineService] instance for managing the user timelines for the various users.
func NewUserTimelineServiceImpl(ctx context.Context, userTimelineCache backend.Cache, userTimelineDB backend.NoSQLDatabase, postStorageService PostStorageService) (UserTimelineService, error) {
	u := &UserTimelineServiceImpl{userTimelineCache: userTimelineCache, userTimelineDB: userTimelineDB, postStorageService: postStorageService}
	return u, nil
}

// Implements UserTimelineService interface
func (u *UserTimelineServiceImpl) ReadUserTimeline(ctx context.Context, reqID int64, userID int64, start int64, stop int64) ([]int64, error) {
	u.NumRequests += 1
	if stop <= start || start < 0 {
		return []int64{}, nil
	}

	userIDStr := strconv.FormatInt(userID, 10)
	var post_ids []int64
	var seen_posts map[int64]bool
	var exists bool

	// Try to use Redis sorted sets if available (much more efficient)
	if redisCache, ok := u.userTimelineCache.(*redis.RedisCache); ok {
		// ZRevRange: get range from sorted set (ordered by score descending)
		// stop-1 because ZRevRange is inclusive on both ends
		postIDStrs, err := redisCache.ZRevRange(ctx, userIDStr, start, stop-1)
		if err != nil {
			// If error, fall through to DB lookup
			postIDStrs = []string{}
		}
		post_ids = make([]int64, 0, len(postIDStrs))
		seen_posts = make(map[int64]bool)
		for _, postIDStr := range postIDStrs {
			postID, parseErr := strconv.ParseInt(postIDStr, 10, 64)
			if parseErr != nil {
				continue
			}
			post_ids = append(post_ids, postID)
			seen_posts[postID] = true
		}
		if len(post_ids) > 0 {
			u.CacheHits++
		} else {
			u.CacheMiss++
		}
	} else {
		// Fallback to old method for non-Redis caches
		var post_infos []PostInfo
		var err error
		exists, err = u.userTimelineCache.Get(ctx, userIDStr, &post_infos)
		if err != nil {
			return []int64{}, err
		}
		if exists {
			u.CacheHits++
		} else {
			u.CacheMiss++
		}
		seen_posts = make(map[int64]bool)
		for _, post_info := range post_infos {
			post_ids = append(post_ids, post_info.PostID)
			seen_posts[post_info.PostID] = true
		}
	}

	db_start := start + int64(len(post_ids))
	var new_post_ids []int64
	var new_post_timestamps map[int64]int64 // Track timestamps for new posts from DB
	if db_start < stop {
		collection, err := u.userTimelineDB.GetCollection(ctx, "usertimeline", "usertimeline")
		if err != nil {
			return []int64{}, err
		}
		query := fmt.Sprintf(`{"UserID": %[1]d}`, userID)
		projection := fmt.Sprintf(`{"posts": {"$slice": [0, %[1]d]}}`, stop)
		query_d, err := parseNoSQLDBQuery(query)
		if err != nil {
			return []int64{}, err
		}
		projection_d, err := parseNoSQLDBQuery(projection)
		if err != nil {
			return []int64{}, err
		}
		post_db_val, err := collection.FindOne(ctx, query_d, projection_d)
		if err != nil {
			return []int64{}, err
		}
		var user_posts UserPosts
		exists, err = post_db_val.One(ctx, &user_posts)
		if err != nil {
			return []int64{}, err
		}
		if !exists {
			// User has no posts in database - return empty slice (not an error)
			// This is consistent with ReadHomeTimeline behavior
			user_posts = UserPosts{UserID: userID, Posts: []PostInfo{}}
		}
		new_post_timestamps = make(map[int64]int64)
		for _, post := range user_posts.Posts {
			// Avoid duplicated post_ids
			if _, ok := seen_posts[post.PostID]; ok {
				continue
			}
			new_post_ids = append(new_post_ids, post.PostID)
			new_post_timestamps[post.PostID] = post.Timestamp
		}
	}

	post_ids = append(new_post_ids, post_ids...)
	post_channel := make(chan bool)
	err_post_channel := make(chan error)
	//var posts []Post
	go func() {
		var err error
		_, err = u.postStorageService.ReadPosts(ctx, reqID, post_ids)
		if err != nil {
			log.Println(err)
			err_post_channel <- err
			return
		}
		post_channel <- true
	}()

	// Update cache with new posts from database
	if len(new_post_ids) > 0 {
		// Try to use Redis sorted sets if available (much more efficient)
		if redisCache, ok := u.userTimelineCache.(*redis.RedisCache); ok {
			// Add each new post to Redis sorted set with its timestamp
			for _, postID := range new_post_ids {
				timestamp, found := new_post_timestamps[postID]
				if !found {
					timestamp = 0 // Fallback if timestamp not found
				}
				postIDStr := strconv.FormatInt(postID, 10)
				err := redisCache.ZAdd(ctx, userIDStr, float64(timestamp), postIDStr)
				if err != nil {
					// Log error but don't fail the request
					log.Printf("ReadUserTimeline: Failed to add post %d to Redis: %v", postID, err)
				}
			}
		} else {
			// Fallback to old method for non-Redis caches
			err := u.userTimelineCache.Put(ctx, userIDStr, post_ids)
			if err != nil {
				return []int64{}, err
			}
		}
	}
	select {
	case <-post_channel:
		break
	case err := <-err_post_channel:
		return []int64{}, err
	}
	return post_ids, nil
}

// Implements UserTimelineService interface
func (u *UserTimelineServiceImpl) WriteUserTimeline(ctx context.Context, reqID int64, postID int64, userID int64, timestamp int64) error {
	log.Printf("WriteUserTimeline: START reqID=%d postID=%d userID=%d", reqID, postID, userID)
	collection, err := u.userTimelineDB.GetCollection(ctx, "usertimeline", "usertimeline")
	if err != nil {
		log.Printf("WriteUserTimeline: GetCollection FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
		return err
	}

	query := bson.D{{"userid", userID}}
	results, err := collection.FindMany(ctx, query)
	var userPosts []UserPosts
	if err != nil {
		log.Printf("WriteUserTimeline: FindMany FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
		return err
	}
	results.All(ctx, &userPosts)

	if len(userPosts) == 0 {
		log.Printf("WriteUserTimeline: Inserting new entry reqID=%d postID=%d userID=%d", reqID, postID, userID)
		userPosts := UserPosts{UserID: userID, Posts: []PostInfo{PostInfo{PostID: postID, Timestamp: timestamp}}}
		err := collection.InsertOne(ctx, userPosts)
		if err != nil {
			log.Printf("WriteUserTimeline: InsertOne FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
			return errors.New("Failed to insert user timeline user to Database")
		}
		log.Printf("WriteUserTimeline: InsertOne SUCCESS reqID=%d postID=%d userID=%d", reqID, postID, userID)
	} else {
		log.Printf("WriteUserTimeline: Updating existing entry reqID=%d postID=%d userID=%d", reqID, postID, userID)
		postIDstr := strconv.FormatInt(postID, 10)
		timestampstr := strconv.FormatInt(timestamp, 10)
		update := fmt.Sprintf(`{"$push": {"Posts": {"$each": [{"PostID": %s, "Timestamp": %s}], "$position": 0}}}`, postIDstr, timestampstr)
		update_d, err := parseNoSQLDBQuery(update)
		if err != nil {
			log.Printf("WriteUserTimeline: parseNoSQLDBQuery FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
			return err
		}
		_, err = collection.UpdateMany(ctx, query, update_d)
		if err != nil {
			log.Printf("WriteUserTimeline: UpdateMany FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
			return errors.New("Failed to insert user timeline user to Database")
		}
		log.Printf("WriteUserTimeline: UpdateMany SUCCESS reqID=%d postID=%d userID=%d", reqID, postID, userID)
	}
	userIDStr := strconv.FormatInt(userID, 10)
	postIDStr := strconv.FormatInt(postID, 10)
	timestampFloat := float64(timestamp)

	// Try to use Redis sorted sets if available (much more efficient)
	if redisCache, ok := u.userTimelineCache.(*redis.RedisCache); ok {
		log.Printf("WriteUserTimeline: Using Redis sorted sets reqID=%d postID=%d userID=%d", reqID, postID, userID)
		err := redisCache.ZAdd(ctx, userIDStr, timestampFloat, postIDStr)
		if err != nil {
			log.Printf("WriteUserTimeline: Redis ZAdd FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
			return err
		}
		log.Printf("WriteUserTimeline: Redis ZAdd SUCCESS reqID=%d postID=%d userID=%d", reqID, postID, userID)
		return nil
	}
	log.Printf("WriteUserTimeline: Using fallback (non-Redis) cache reqID=%d postID=%d userID=%d", reqID, postID, userID)

	// Fallback to old read-modify-write pattern for non-Redis caches
	var postInfo []PostInfo
	// Ignore error check for Get!
	_, err = u.userTimelineCache.Get(ctx, userIDStr, &postInfo)
	if err != nil {
		log.Printf("WriteUserTimeline: Cache Get FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
		return err
	}
	postInfo = append(postInfo, PostInfo{PostID: postID, Timestamp: timestamp})
	// Limit cached timeline size to prevent unbounded growth
	// This keeps cache operations O(1) instead of O(n) where n = number of posts
	const maxCachedTimelineSize = 5000
	if len(postInfo) > maxCachedTimelineSize {
		postInfo = postInfo[len(postInfo)-maxCachedTimelineSize:]
	}
	err = u.userTimelineCache.Put(ctx, userIDStr, postInfo)
	if err != nil {
		log.Printf("WriteUserTimeline: Cache Put FAILED reqID=%d postID=%d userID=%d error=%v", reqID, postID, userID, err)
		return err
	}
	log.Printf("WriteUserTimeline: Fallback cache SUCCESS reqID=%d postID=%d userID=%d", reqID, postID, userID)
	return nil
}
