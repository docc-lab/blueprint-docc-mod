package snnw

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// The UserIDService interface
type UserIDService interface {
	// Returns the userID of the user associated with the `username`.
	// Returns an error if no user exists with the given `username`.
	GetUserId(ctx context.Context, reqID int64, username string) (int64, error)
}

// Implementation of [UserIDService]
type UserIDServiceImpl struct {
	userCache backend.Cache
	userDB    backend.NoSQLDatabase
}

// Creates a [UserIDService] instance for looking up users with usernames.
func NewUserIDServiceImpl(ctx context.Context, userCache backend.Cache, userDB backend.NoSQLDatabase) (UserIDService, error) {
	return &UserIDServiceImpl{userCache: userCache, userDB: userDB}, nil
}

// Implements UserIDService interface
func (u *UserIDServiceImpl) GetUserId(ctx context.Context, reqID int64, username string) (int64, error) {
	return 0, nil
}
