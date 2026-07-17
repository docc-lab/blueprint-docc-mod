package snnw

import (
	"context"
	"sync"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// The SocialGraphService interface
type SocialGraphService interface {
	// Returns the user IDs of all the followers of the user with user id `userID`.
	// Returns an error if user with `userID` doesn't exist in database.
	GetFollowers(ctx context.Context, reqID int64, userID int64) ([]int64, error)
	// Returns the user IDs of all the followees of the user with user id `userID`.
	// Returns an error if user with `userID` doesn't exist in database.
	GetFollowees(ctx context.Context, reqID int64, userID int64) ([]int64, error)
	// Creates a follower-followee relationship between users with IDs `userID`-`followeeID`.
	Follow(ctx context.Context, reqID int64, userID int64, followeeID int64) error
	// Removes the follower-followee relationship between users with IDs `userID`-`followeeID`.
	Unfollow(ctx context.Context, reqID int64, userID int64, followeeID int64) error
	// Creates a follower-followee relationship between users with usernames `userUsername`-`followeeUsername`.
	FollowWithUsername(ctx context.Context, reqID int64, userUsername string, followeeUsername string) error
	// Removes the follower-followee relationship between users with usernames `userUsername`-`followeeUsername`.
	UnfollowWithUsername(ctx context.Context, reqID int64, userUsername string, followeeUsername string) error
	// Inserts a new user with `userID` in the database.
	InsertUser(ctx context.Context, reqID int64, userID int64) error
}

// The format of a follower's info stored in the user info in the social-graph
type FollowerInfo struct {
	FollowerID int64
	Timestamp  int64
}

// The format of a followee's info stored in the user info in the social-graph
type FolloweeInfo struct {
	FolloweeID int64
	Timestamp  int64
}

// The format of a user's info stored in the social-graph
type UserInfo struct {
	UserID    int64
	Followers []FollowerInfo
	Followees []FolloweeInfo
}

// Implementation of [SocialGraphService]
type SocialGraphServiceImpl struct {
	socialGraphCache backend.Cache
	socialGraphDB    backend.NoSQLDatabase
	userIDService    UserIDService
}

// Creates a [SocialGraphService] instance that maintains the social graph backends.
func NewSocialGraphServiceImpl(ctx context.Context, socialGraphCache backend.Cache, socialGraphDB backend.NoSQLDatabase, userIDService UserIDService) (SocialGraphService, error) {
	return &SocialGraphServiceImpl{socialGraphCache: socialGraphCache, socialGraphDB: socialGraphDB, userIDService: userIDService}, nil
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) GetFollowers(ctx context.Context, reqID int64, userID int64) ([]int64, error) {
	return []int64{}, nil
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) GetFollowees(ctx context.Context, reqID int64, userID int64) ([]int64, error) {
	return []int64{}, nil
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) Follow(ctx context.Context, reqID int64, userID int64, followeeID int64) error {
	return nil
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) Unfollow(ctx context.Context, reqID int64, userID int64, followeeID int64) error {
	return nil
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) FollowWithUsername(ctx context.Context, reqID int64, username string, followee_name string) error {
	var id int64
	var followee_id int64
	var err1 error
	var err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	// Preserve downstream fan-out to UserIDService (traced call graph).
	go func() {
		defer wg.Done()
		id, err1 = s.userIDService.GetUserId(ctx, reqID, username)
	}()
	go func() {
		defer wg.Done()
		followee_id, err2 = s.userIDService.GetUserId(ctx, reqID, followee_name)
	}()
	wg.Wait()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return s.Follow(ctx, reqID, id, followee_id)
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) UnfollowWithUsername(ctx context.Context, reqID int64, username string, followee_name string) error {
	var id int64
	var followee_id int64
	var err1 error
	var err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	// Preserve downstream fan-out to UserIDService (traced call graph).
	go func() {
		defer wg.Done()
		id, err1 = s.userIDService.GetUserId(ctx, reqID, username)
	}()
	go func() {
		defer wg.Done()
		followee_id, err2 = s.userIDService.GetUserId(ctx, reqID, followee_name)
	}()
	wg.Wait()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return s.Unfollow(ctx, reqID, id, followee_id)
}

// Implements SocialGraphService interface
func (s *SocialGraphServiceImpl) InsertUser(ctx context.Context, reqID int64, userID int64) error {
	return nil
}
