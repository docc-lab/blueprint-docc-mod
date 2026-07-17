package snnw

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
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
	post_ids := []int64{}
	// Preserve downstream call to PostStorageService (traced call graph).
	_, err := u.postStorageService.ReadPosts(ctx, reqID, post_ids)
	if err != nil {
		return []int64{}, err
	}
	return post_ids, nil
}

// Implements UserTimelineService interface
func (u *UserTimelineServiceImpl) WriteUserTimeline(ctx context.Context, reqID int64, postID int64, userID int64, timestamp int64) error {
	return nil
}
