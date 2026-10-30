package socialnetwork

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync" // FIX (4): per-user locks for WriteUserTimeline

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
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
	userLocks          sync.Map // FIX (4): userID -> *sync.Mutex, see lockUser
}

// FIX (6): maximum number of entries kept in each user timeline, both in the
// cache and in the database document. WriteUserTimeline reads and rewrites the
// whole cached list and loads the whole database document on every post, so
// without a limit the cost of each post grows with the user's post count.
// Keeping only the newest userTimelineCap entries bounds that cost. The posts
// themselves stay in post storage; only the timeline index is bounded.
const userTimelineCap = 100

// FIX (4): WriteUserTimeline does "check whether the user has a timeline, then
// insert or update" in the database, and "read the cached list, append, write
// it back" in the cache. With concurrent requests (which gRPC allows), two
// posts by the same user could both see "no timeline yet" and insert two
// timeline documents, and could overwrite each other's cache update. Holding a
// per-user lock makes each user's writes take turns, while writes for
// different users still run in parallel. This assumes a single replica of
// this service (as deployed), since the lock lives in this process.
func (u *UserTimelineServiceImpl) lockUser(userID int64) func() {
	m, _ := u.userLocks.LoadOrStore(userID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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
	var post_infos []PostInfo
	exists, err := u.userTimelineCache.Get(ctx, userIDStr, &post_infos)
	if err != nil {
		return []int64{}, err
	}
	if exists {
		u.CacheHits += 1
	} else {
		u.CacheMiss += 1
	}
	var post_ids []int64
	seen_posts := make(map[int64]bool)
	for _, post_info := range post_infos {
		post_ids = append(post_ids, post_info.PostID)
		seen_posts[post_info.PostID] = true
	}
	db_start := start + int64(len(post_ids))
	var new_post_ids []int64
	// FIX (1/3): also keep the full PostInfo entries read from the database, so the
	// cache can be written in the same []PostInfo format every other cache access uses.
	var new_post_infos []PostInfo
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
			return []int64{}, errors.New("Failed to find posts in database")
		}
		for _, post := range user_posts.Posts {
			// Avoid duplicated post_ids
			if _, ok := seen_posts[post.PostID]; ok {
				continue
			}
			new_post_ids = append(new_post_ids, post.PostID)
			// FIX (2/3): collect the matching PostInfo alongside its ID.
			new_post_infos = append(new_post_infos, post)
		}
	}

	post_ids = append(new_post_ids, post_ids...)
	fmt.Println(post_ids)
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

	if len(new_post_ids) > 0 {
		// FIX (3/3): previously this cached post_ids ([]int64), but the cache is read
		// back as []PostInfo (above, and in WriteUserTimeline). That made every cache
		// hit fail with "json: cannot unmarshal number into Go value of type
		// socialnetwork.PostInfo". Cache []PostInfo instead, in the same order as
		// post_ids: new entries from the database first, then the ones already cached.
		err := u.userTimelineCache.Put(ctx, userIDStr, append(new_post_infos, post_infos...))
		if err != nil {
			return []int64{}, err
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
	defer u.lockUser(userID)() // FIX (4): one write at a time per user
	collection, err := u.userTimelineDB.GetCollection(ctx, "usertimeline", "usertimeline")
	if err != nil {
		return err
	}

	query := bson.D{{"userid", userID}}
	results, err := collection.FindMany(ctx, query)
	var userPosts []UserPosts
	if err != nil {
		return err
	}
	results.All(ctx, &userPosts)

	if len(userPosts) == 0 {
		fmt.Println("Inserting new entry for", userID)
		userPosts := UserPosts{UserID: userID, Posts: []PostInfo{PostInfo{PostID: postID, Timestamp: timestamp}}}
		err := collection.InsertOne(ctx, userPosts)
		if err != nil {
			return errors.New("Failed to insert user timeline user to Database")
		}
	} else {
		fmt.Println("Adding a new post for user", userID)
		postIDstr := strconv.FormatInt(postID, 10)
		timestampstr := strconv.FormatInt(timestamp, 10)
		// FIX (6): "$slice" keeps only the newest userTimelineCap entries (new entries go to position 0)
		update := fmt.Sprintf(`{"$push": {"Posts": {"$each": [{"PostID": %s, "Timestamp": %s}], "$position": 0, "$slice": %d}}}`, postIDstr, timestampstr, userTimelineCap)
		update_d, err := parseNoSQLDBQuery(update)
		if err != nil {
			return err
		}
		_, err = collection.UpdateMany(ctx, query, update_d)
		if err != nil {
			return errors.New("Failed to insert user timeline user to Database")
		}
	}
	var postInfo []PostInfo
	userIDStr := strconv.FormatInt(userID, 10)
	// Ignore error check for Get!
	_, err = u.userTimelineCache.Get(ctx, userIDStr, &postInfo)
	if err != nil {
		return err
	}
	postInfo = append(postInfo, PostInfo{PostID: postID, Timestamp: timestamp})
	if len(postInfo) > userTimelineCap { // FIX (6): keep only the newest entries (appended at the end)
		postInfo = postInfo[len(postInfo)-userTimelineCap:]
	}
	return u.userTimelineCache.Put(ctx, userIDStr, postInfo)
}