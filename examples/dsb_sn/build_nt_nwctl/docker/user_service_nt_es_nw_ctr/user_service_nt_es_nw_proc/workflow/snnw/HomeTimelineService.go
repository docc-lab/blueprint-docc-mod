package snnw

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
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
	// Preserve downstream call to SocialGraphService (traced call graph).
	_, err := h.socialGraphService.GetFollowers(ctx, reqID, userID)
	if err != nil {
		return err
	}
	return nil
}

// Implements HomeTimelineService interface
func (h *HomeTimelineServiceImpl) ReadHomeTimeline(ctx context.Context, reqID int64, userID int64, start int64, stop int64) ([]int64, error) {
	postIDs := []int64{}
	// Preserve downstream call to PostStorageService (traced call graph).
	_, err := h.postStorageService.ReadPosts(ctx, reqID, postIDs)
	if err != nil {
		return []int64{}, err
	}
	return postIDs, nil
}
