package socialnetwork

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.mongodb.org/mongo-driver/bson"
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
	collection, err := p.postStorageDB.GetCollection(ctx, "post", "post")
	if err != nil {
		return err
	}
	return collection.InsertOne(ctx, post)
}

// Implements PostStorageService interface
func (p *PostStorageServiceImpl) ReadPost(ctx context.Context, reqID int64, postID int64) (Post, error) {
	var post Post
	exists, err := p.postStorageCache.Get(ctx, strconv.FormatInt(postID, 10), &post)
	if err != nil {
		return post, err
	}
	if !exists {
		// Post was not in Cache, check DB!
		collection, err := p.postStorageDB.GetCollection(ctx, "post", "post")
		if err != nil {
			return post, err
		}
		query := bson.D{{"postid", postID}}
		result, err := collection.FindOne(ctx, query)
		if err != nil {
			return post, err
		}
		res, err := result.One(ctx, &post)
		if !res || err != nil {
			return post, errors.New("Post doesn't exist in MongoDB")
		}
	}
	return post, nil
}

// Implements PostStorageService interface
func (p *PostStorageServiceImpl) ReadPosts(ctx context.Context, reqID int64, postIDs []int64) ([]Post, error) {
	unique_post_ids := make(map[int64]bool)
	for _, pid := range postIDs {
		unique_post_ids[pid] = true
	}
	//if len(unique_post_ids) != len(postIDs) {
	//	return []Post{}, errors.New("Post Ids are duplicated")
	//}
	var keys []string
	for _, pid := range postIDs {
		keys = append(keys, strconv.FormatInt(pid, 10))
	}
	values := make([]Post, len(keys))
	var retvals []interface{}
	for idx, _ := range values {
		retvals = append(retvals, &values[idx])
	}
	p.postStorageCache.Mget(ctx, keys, retvals)
	var retposts []Post
	for _, post := range values {
		if post.PostID != 0 {
			delete(unique_post_ids, post.PostID)
			retposts = append(retposts, post)
		}
	}
	p.NumReqs += 1
	if len(unique_post_ids) != 0 {
		p.CacheMiss += 1
		var new_posts []Post
		var unique_pids []int64
		for k := range unique_post_ids {
			unique_pids = append(unique_pids, k)
		}
		collection, err := p.postStorageDB.GetCollection(ctx, "post", "post")
		if err != nil {
			return []Post{}, err
		}
		id_str := strings.Join(strings.Fields(fmt.Sprint(unique_pids)), ",")
		query := `{"PostID": {"$in": ` + id_str + `}}`
		query_d, err := parseNoSQLDBQuery(query)
		if err != nil {
			return []Post{}, err
		}
		vals, err := collection.FindMany(ctx, query_d)
		if err != nil {
			log.Println(err)
			return []Post{}, err
		}
		err = vals.All(ctx, &new_posts)
		if err != nil {
			return []Post{}, err
		}
		retposts = append(retposts, new_posts...)
		// Cache new posts asynchronously using batch Mset operation
		// Like the original DeathStarBench implementation, we wait for cache writes to complete
		if len(new_posts) > 0 {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Compute keys/values and do Mset in the goroutine
				// This keeps all cache work together and allows marshaling to happen in background
				cache_keys := make([]string, len(new_posts))
				cache_values := make([]interface{}, len(new_posts))
				for idx, new_post := range new_posts {
					cache_keys[idx] = strconv.FormatInt(new_post.PostID, 10)
					cache_values[idx] = new_post
				}
				// Use background context since this runs in a goroutine
				err := p.postStorageCache.Mset(context.Background(), cache_keys, cache_values)
				if err != nil {
					// Log error but don't fail the request - cache is best effort
					log.Printf("PostStorageService: Mset failed for %d posts: %v", len(new_posts), err)
				}
			}()
			wg.Wait()
		}
	}
	return retposts, nil
}
