package snnw

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// The PostStorageService interface
type PostStorageService interface {
	// Stores a new `post` in the relevant backends.
	StorePost(ctx context.Context, reqID int64, post Post) error
	// Returns the post with id `postID`
	// If no post with id `postID` exists in the database, an error is returned.
	ReadPost(ctx context.Context, reqID int64, postID int64) (Post, error)
	// Returns a list of posts that have ids in the array `postIDs`.
	// No error is thrown if no post is found.
	ReadPosts(ctx context.Context, reqID int64, postIDs []int64) ([]Post, error)
}

// Implementation of [PostStorageService]
type PostStorageServiceImpl struct {
	postStorageCache backend.Cache
	postStorageDB    backend.NoSQLDatabase
	CacheHits        int64
	NumReqs          int64
	CacheMiss        int64
}

// Creates a [PostStorageService] instance that manages the post backends.
func NewPostStorageServiceImpl(ctx context.Context, postStorageCache backend.Cache, postStorageDB backend.NoSQLDatabase) (PostStorageService, error) {
	p := &PostStorageServiceImpl{postStorageCache: postStorageCache, postStorageDB: postStorageDB}
	return p, nil
}

// Implements PostStorageService interface
func (p *PostStorageServiceImpl) StorePost(ctx context.Context, reqID int64, post Post) error {
	return nil
}

// Implements PostStorageService interface
func (p *PostStorageServiceImpl) ReadPost(ctx context.Context, reqID int64, postID int64) (Post, error) {
	return Post{}, nil
}

// Implements PostStorageService interface
func (p *PostStorageServiceImpl) ReadPosts(ctx context.Context, reqID int64, postIDs []int64) ([]Post, error) {
	return []Post{}, nil
}
