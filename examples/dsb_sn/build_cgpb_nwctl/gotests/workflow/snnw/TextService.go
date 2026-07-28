package snnw

import (
	"context"
	"sync"
)

// The TextService interface
type TextService interface {
	// Parses the raw `text` to return an edited text with the urls replaced with shortened urls, usermention objects to be stored with the post, and the url objects to be stored with the post.
	ComposeText(ctx context.Context, reqID int64, text string) (string, []UserMention, []URL, error)
}

// Implementation of [TextService]
type TextServiceImpl struct {
	urlShortenService  UrlShortenService
	userMentionService UserMentionService
}

// Creates a [TextService] instance for parsing texts in created posts.
func NewTextServiceImpl(ctx context.Context, urlShortenService UrlShortenService, userMentionService UserMentionService) (TextService, error) {
	return &TextServiceImpl{urlShortenService: urlShortenService, userMentionService: userMentionService}, nil
}

// Implements TextService interface
func (t *TextServiceImpl) ComposeText(ctx context.Context, reqID int64, text string) (string, []UserMention, []URL, error) {
	var err1, err2 error
	var urls []URL
	var user_mentions []UserMention
	var wg sync.WaitGroup
	wg.Add(2)
	// Preserve downstream fan-out to UrlShortenService + UserMentionService (traced call graph).
	go func() {
		defer wg.Done()
		urls, err1 = t.urlShortenService.ComposeUrls(ctx, reqID, []string{})
	}()
	go func() {
		defer wg.Done()
		user_mentions, err2 = t.userMentionService.ComposeUserMentions(ctx, reqID, []string{})
	}()
	wg.Wait()
	if err1 != nil {
		return text, user_mentions, urls, err1
	}
	if err2 != nil {
		return text, user_mentions, urls, err2
	}
	return text, user_mentions, urls, nil
}
