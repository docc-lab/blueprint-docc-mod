package snnw

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// The UserMentionService interface
type UserMentionService interface {
	// Composes UserMention objects to be stored in a post by converting raw usernames.
	// Returns an error if any name in the `usernames` array is not registered.
	ComposeUserMentions(ctx context.Context, reqID int64, usernames []string) ([]UserMention, error)
}

// Implementation of [UserMentionService]
type UserMentionServiceImpl struct {
	userCache backend.Cache
	userDB    backend.NoSQLDatabase
}

// Creates a [UserMentionService] instance that is responsible for converting usernames to usermention objects
func NewUserMentionServiceImpl(ctx context.Context, userCache backend.Cache, userDB backend.NoSQLDatabase) (UserMentionService, error) {
	return &UserMentionServiceImpl{userCache: userCache, userDB: userDB}, nil
}

// Implements the UserMentionService interface
func (u *UserMentionServiceImpl) ComposeUserMentions(ctx context.Context, reqID int64, usernames []string) ([]UserMention, error) {
	return []UserMention{}, nil
}
