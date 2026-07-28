package snnw

import (
	"context"
)

// The MediaService interface
type MediaService interface {
	// Creates and returns a list of Media objects from the provided `mediaTypes` and `mediaIds` arguments.
	// Returns an error if the length of mediaTypes and mediaIds do not match.
	ComposeMedia(ctx context.Context, reqID int64, mediaTypes []string, mediaIds []int64) ([]Media, error)
}

// Implementation of [MediaService]
type MediaServiceImpl struct{}

// Creates a [MediaService] instance that creates media objects
func NewMediaServiceImpl(ctx context.Context) (MediaService, error) {
	return &MediaServiceImpl{}, nil
}

// Implements MediaService interface
func (m *MediaServiceImpl) ComposeMedia(ctx context.Context, reqID int64, mediaTypes []string, mediaIds []int64) ([]Media, error) {
	return []Media{}, nil
}
