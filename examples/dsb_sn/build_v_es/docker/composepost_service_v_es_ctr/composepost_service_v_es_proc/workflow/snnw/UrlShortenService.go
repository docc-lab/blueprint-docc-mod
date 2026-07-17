package snnw

import (
	"context"
	"math/rand"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// The UrlShortenService interface
type UrlShortenService interface {
	// Converts raw `urls` into shortened urls to be used within the application. Returns the list of shortened urls.
	ComposeUrls(ctx context.Context, reqID int64, urls []string) ([]URL, error)
	// Converts the list of shortened urls into their extended forms.
	GetExtendedUrls(ctx context.Context, reqID int64, shortenedUrls []string) ([]string, error)
}

// Implementation of [UrlShortenService]
type UrlShortenServiceImpl struct {
	urlShortenDB backend.NoSQLDatabase
	hostname     string
}

// Creates a [UrlShortenService] instance for converting raw urls to shortened urls and vice versa.
func NewUrlShortenServiceImpl(ctx context.Context, urlShortenDB backend.NoSQLDatabase) (UrlShortenService, error) {
	rand.Seed(time.Now().UnixNano())
	return &UrlShortenServiceImpl{urlShortenDB: urlShortenDB, hostname: "http://short-url/"}, nil
}

// Implements ComposeUrls interface
func (u *UrlShortenServiceImpl) ComposeUrls(ctx context.Context, reqID int64, urls []string) ([]URL, error) {
	return []URL{}, nil
}

// Implements UrlShortenService interface.
// Currently not implemented as the original DSB application doesn't implement this function either.
func (u *UrlShortenServiceImpl) GetExtendedUrls(ctx context.Context, reqID int64, shortenedUrls []string) ([]string, error) {
	// Not implemented in Original DSB
	return []string{}, nil
}
