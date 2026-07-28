package snnw

import (
	"context"
)

// The UniqueIdService interface
type UniqueIdService interface {
	// Returns a newly generated unique id to be used as a post's unique identifier.
	ComposeUniqueId(ctx context.Context, reqID int64, postType int64) (int64, error)
}

// Implementation of [UserTimelineService]
type UniqueIdServiceImpl struct {
	counter           int64
	current_timestamp int64
	machine_id        string
}

// Implements UniqueIdService interface
func NewUniqueIdServiceImpl(ctx context.Context) (UniqueIdService, error) {
	return &UniqueIdServiceImpl{counter: 0, current_timestamp: -1, machine_id: GetMachineID()}, nil
}

// Implements UniqueIdService interface
func (u *UniqueIdServiceImpl) ComposeUniqueId(ctx context.Context, reqID int64, postType int64) (int64, error) {
	return 0, nil
}
