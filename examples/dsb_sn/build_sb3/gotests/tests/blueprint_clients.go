
package tests

import (
	"github.com/blueprint-uservices/blueprint/examples/dsb_sn/workflow/socialnetwork"
	"context"
	"blueprint/testclients/clients"
	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// Auto-generated code by the Blueprint gotests plugin.
func init() {
	// Initialize the clientlib early so that it can pick up command-line flags
	clientlib := clients.NewClientLibrary("tests")

	
	socialGraphDBRegistry.Register("user_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("user_db_sb3.client", &client)
		return client, err
	})
	
	userDBRegistry.Register("user_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("user_db_sb3.client", &client)
		return client, err
	})
	
	postDBRegistry.Register("user_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("user_db_sb3.client", &client)
		return client, err
	})
	
	userTimelineDBRegistry.Register("user_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("user_db_sb3.client", &client)
		return client, err
	})
	
	homeTimelineCacheRegistry.Register("post_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("post_cache_sb3.client", &client)
		return client, err
	})
	
	userCacheRegistry.Register("post_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("post_cache_sb3.client", &client)
		return client, err
	})
	
	socialGraphCacheRegistry.Register("post_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("post_cache_sb3.client", &client)
		return client, err
	})
	
	userTimelineCacheRegistry.Register("post_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("post_cache_sb3.client", &client)
		return client, err
	})
	
	postCacheRegistry.Register("post_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("post_cache_sb3.client", &client)
		return client, err
	})
	
	urlShortenServiceRegistry.Register("urlshorten_service_sb3", func(ctx context.Context) (socialnetwork.UrlShortenService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UrlShortenService
		err = namespace.Get("urlshorten_service_sb3.client", &client)
		return client, err
	})
	
	userIDServiceRegistry.Register("userid_service_sb3", func(ctx context.Context) (socialnetwork.UserIDService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UserIDService
		err = namespace.Get("userid_service_sb3.client", &client)
		return client, err
	})
	
	homeTimelineServiceRegistry.Register("hometimeline_service_sb3", func(ctx context.Context) (socialnetwork.HomeTimelineService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.HomeTimelineService
		err = namespace.Get("hometimeline_service_sb3.client", &client)
		return client, err
	})
	
	userServiceRegistry.Register("user_service_sb3", func(ctx context.Context) (socialnetwork.UserService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UserService
		err = namespace.Get("user_service_sb3.client", &client)
		return client, err
	})
	
	socialGraphDBRegistry.Register("social_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("social_db_sb3.client", &client)
		return client, err
	})
	
	userDBRegistry.Register("social_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("social_db_sb3.client", &client)
		return client, err
	})
	
	postDBRegistry.Register("social_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("social_db_sb3.client", &client)
		return client, err
	})
	
	userTimelineDBRegistry.Register("social_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("social_db_sb3.client", &client)
		return client, err
	})
	
	socialGraphDBRegistry.Register("usertimeline_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("usertimeline_db_sb3.client", &client)
		return client, err
	})
	
	userDBRegistry.Register("usertimeline_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("usertimeline_db_sb3.client", &client)
		return client, err
	})
	
	postDBRegistry.Register("usertimeline_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("usertimeline_db_sb3.client", &client)
		return client, err
	})
	
	userTimelineDBRegistry.Register("usertimeline_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("usertimeline_db_sb3.client", &client)
		return client, err
	})
	
	postStorageServiceRegistry.Register("post_storage_service_sb3", func(ctx context.Context) (socialnetwork.PostStorageService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.PostStorageService
		err = namespace.Get("post_storage_service_sb3.client", &client)
		return client, err
	})
	
	homeTimelineCacheRegistry.Register("user_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("user_cache_sb3.client", &client)
		return client, err
	})
	
	userCacheRegistry.Register("user_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("user_cache_sb3.client", &client)
		return client, err
	})
	
	socialGraphCacheRegistry.Register("user_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("user_cache_sb3.client", &client)
		return client, err
	})
	
	userTimelineCacheRegistry.Register("user_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("user_cache_sb3.client", &client)
		return client, err
	})
	
	postCacheRegistry.Register("user_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("user_cache_sb3.client", &client)
		return client, err
	})
	
	homeTimelineCacheRegistry.Register("social_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("social_cache_sb3.client", &client)
		return client, err
	})
	
	userCacheRegistry.Register("social_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("social_cache_sb3.client", &client)
		return client, err
	})
	
	socialGraphCacheRegistry.Register("social_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("social_cache_sb3.client", &client)
		return client, err
	})
	
	userTimelineCacheRegistry.Register("social_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("social_cache_sb3.client", &client)
		return client, err
	})
	
	postCacheRegistry.Register("social_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("social_cache_sb3.client", &client)
		return client, err
	})
	
	homeTimelineCacheRegistry.Register("usertimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("usertimeline_cache_sb3.client", &client)
		return client, err
	})
	
	userCacheRegistry.Register("usertimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("usertimeline_cache_sb3.client", &client)
		return client, err
	})
	
	socialGraphCacheRegistry.Register("usertimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("usertimeline_cache_sb3.client", &client)
		return client, err
	})
	
	userTimelineCacheRegistry.Register("usertimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("usertimeline_cache_sb3.client", &client)
		return client, err
	})
	
	postCacheRegistry.Register("usertimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("usertimeline_cache_sb3.client", &client)
		return client, err
	})
	
	homeTimelineCacheRegistry.Register("hometimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("hometimeline_cache_sb3.client", &client)
		return client, err
	})
	
	userCacheRegistry.Register("hometimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("hometimeline_cache_sb3.client", &client)
		return client, err
	})
	
	socialGraphCacheRegistry.Register("hometimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("hometimeline_cache_sb3.client", &client)
		return client, err
	})
	
	userTimelineCacheRegistry.Register("hometimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("hometimeline_cache_sb3.client", &client)
		return client, err
	})
	
	postCacheRegistry.Register("hometimeline_cache_sb3", func(ctx context.Context) (backend.Cache, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.Cache
		err = namespace.Get("hometimeline_cache_sb3.client", &client)
		return client, err
	})
	
	mediaServiceRegistry.Register("media_service_sb3", func(ctx context.Context) (socialnetwork.MediaService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.MediaService
		err = namespace.Get("media_service_sb3.client", &client)
		return client, err
	})
	
	textServiceRegistry.Register("text_service_sb3", func(ctx context.Context) (socialnetwork.TextService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.TextService
		err = namespace.Get("text_service_sb3.client", &client)
		return client, err
	})
	
	composePostServiceRegistry.Register("composepost_service_sb3", func(ctx context.Context) (socialnetwork.ComposePostService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.ComposePostService
		err = namespace.Get("composepost_service_sb3.client", &client)
		return client, err
	})
	
	socialGraphDBRegistry.Register("post_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("post_db_sb3.client", &client)
		return client, err
	})
	
	userDBRegistry.Register("post_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("post_db_sb3.client", &client)
		return client, err
	})
	
	postDBRegistry.Register("post_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("post_db_sb3.client", &client)
		return client, err
	})
	
	userTimelineDBRegistry.Register("post_db_sb3", func(ctx context.Context) (backend.NoSQLDatabase, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client backend.NoSQLDatabase
		err = namespace.Get("post_db_sb3.client", &client)
		return client, err
	})
	
	userMentionServiceRegistry.Register("usermention_service_sb3", func(ctx context.Context) (socialnetwork.UserMentionService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UserMentionService
		err = namespace.Get("usermention_service_sb3.client", &client)
		return client, err
	})
	
	uniqueIdServiceRegistry.Register("uniqueid_service_sb3", func(ctx context.Context) (socialnetwork.UniqueIdService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UniqueIdService
		err = namespace.Get("uniqueid_service_sb3.client", &client)
		return client, err
	})
	
	socialGraphServiceRegistry.Register("socialgraph_service_sb3", func(ctx context.Context) (socialnetwork.SocialGraphService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.SocialGraphService
		err = namespace.Get("socialgraph_service_sb3.client", &client)
		return client, err
	})
	
	userTimelineServiceRegistry.Register("usertimeline_service_sb3", func(ctx context.Context) (socialnetwork.UserTimelineService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.UserTimelineService
		err = namespace.Get("usertimeline_service_sb3.client", &client)
		return client, err
	})
	
	wrk2apiServiceRegistry.Register("wrk2api_service_sb3", func(ctx context.Context) (socialnetwork.Wrk2APIService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client socialnetwork.Wrk2APIService
		err = namespace.Get("wrk2api_service_sb3.client", &client)
		return client, err
	})
	
}
