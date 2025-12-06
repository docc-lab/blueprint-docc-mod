# Operation Complexity Analysis: SockShop vs DSB_SN

This document analyzes the complexity of operations in both the SockShop and DSB_SN (DeathStarBench Social Network) examples in the Blueprint project.

## Complexity Metrics

We measure complexity using the following metrics:
- **Service Calls**: Number of downstream service calls made
- **Concurrent Operations**: Whether operations can be parallelized
- **Database Operations**: Number of database reads/writes
- **Dependencies**: Number of services that must be called
- **Fan-out**: Number of entities affected (e.g., users, timelines)

---

## SockShop Example

### Service Architecture
- **Frontend**: Gateway service that routes requests
- **Catalogue**: Product catalog management
- **Cart**: Shopping cart management
- **User**: User accounts, addresses, and payment cards
- **Order**: Order processing and management
- **Payment**: Payment authorization
- **Shipping**: Shipment management

### Operation Complexity Analysis

#### Simple Operations (1-2 Service Calls)

| Operation | Service Calls | Database Ops | Complexity | Notes |
|----------|--------------|--------------|------------|-------|
| `GetSock` | 1 | 1 read | Low | Direct catalogue lookup |
| `ListItems` | 1 | 1 read | Low | Catalogue query with filters |
| `ListTags` | 1 | 1 read | Low | Simple tag retrieval |
| `GetCart` | 1 | 1 read | Low | Direct cart retrieval |
| `GetUser` | 1 | 1-2 reads | Low | User lookup, may include addresses/cards |
| `GetAddress` | 1 | 1 read | Low | Direct address lookup |
| `GetCard` | 1 | 1 read | Low | Direct card lookup |
| `GetOrder` | 1 | 1 read | Low | Direct order lookup |
| `GetOrders` | 1 | 1 read | Low | Query orders by user |
| `RemoveItem` | 1 | 1-2 writes | Low | Cart update, may delete cart if empty |
| `DeleteCart` | 1 | 1 write | Low | Simple deletion |

#### Medium Complexity Operations (2-3 Service Calls)

| Operation | Service Calls | Database Ops | Complexity | Notes |
|----------|--------------|--------------|------------|-------|
| `AddItem` | 2 | 1 read + 1 write | Medium | Catalogue lookup + cart update |
| `UpdateItem` | 2 | 1 read + 1 write | Medium | Catalogue lookup + cart update |
| `Login` | 1-2 | 1-2 reads | Medium | User auth + optional cart merge |
| `Register` | 1-2 | 1 write + optional merge | Medium | User creation + optional cart merge |
| `PostAddress` | 1 | 1 write | Medium | Address creation |
| `PostCard` | 1 | 1 write | Medium | Card creation |

#### High Complexity Operations (4+ Service Calls)

| Operation | Service Calls | Database Ops | Concurrent? | Complexity | Notes |
|----------|--------------|--------------|-------------|-----------|-------|
| `NewOrder` | 5 | 4 reads + 2 writes | Yes (4 parallel) | High | Fetches cart, user, address, card concurrently; then payment + shipping + order creation |

**NewOrder Breakdown:**
1. **Concurrent Phase (4 parallel calls):**
   - `GetCart(cartID)` - Fetch cart items
   - `GetUsers(customerID)` - Fetch customer info
   - `GetAddresses(addressID)` - Fetch shipping address
   - `GetCards(cardID)` - Fetch payment card
2. **Sequential Phase:**
   - `Authorise(amount)` - Payment authorization
   - `PostShipping(shipment)` - Create shipment
   - `InsertOne(order)` - Save order
   - `DeleteCart(customerID)` - Clear cart

**Complexity Factors:**
- Requires coordination of 4 different data sources
- Payment authorization adds external dependency
- Transactional concerns (order creation + cart deletion)
- Error handling across multiple services

---

## DSB_SN (DeathStarBench Social Network) Example

### Service Architecture
- **Wrk2APIService**: Frontend gateway
- **ComposePostService**: Post composition orchestrator
- **UserService**: User management and authentication
- **PostStorageService**: Post persistence
- **UserTimelineService**: User's own timeline
- **HomeTimelineService**: Aggregated timeline of followed users
- **SocialGraphService**: Follower/followee relationships
- **TextService**: Text processing (URL shortening, mentions)
- **MediaService**: Media object creation
- **UniqueIdService**: ID generation
- **UrlShortenService**: URL shortening
- **UserMentionService**: User mention extraction
- **UserIdService**: Username to ID lookup

### Operation Complexity Analysis

#### Simple Operations (1-2 Service Calls)

| Operation | Service Calls | Database Ops | Complexity | Notes |
|----------|--------------|--------------|------------|-------|
| `ReadUserTimeline` | 1-2 | 1-2 reads | Low-Medium | Timeline read + optional post fetch |
| `GetUserId` | 1 | 1 read | Low | Username lookup |
| `ComposeMedia` | 0 | 0 | Low | In-memory object creation |
| `ComposeUniqueId` | 0 | 0 | Low | ID generation (local) |

#### Medium Complexity Operations (2-4 Service Calls)

| Operation | Service Calls | Database Ops | Concurrent? | Complexity | Notes |
|----------|--------------|--------------|-------------|-----------|-------|
| `ReadHomeTimeline` | 2 | 1-2 reads | No | Medium | Timeline read + post fetch |
| `Follow` | 2-3 | 2 writes | Yes (2 parallel) | Medium | Updates both follower and followee records |
| `Unfollow` | 2-3 | 2 writes | Yes (2 parallel) | Medium | Removes from both records |
| `Register` | 1-2 | 1-2 writes | No | Medium | User creation + optional social graph insert |
| `Login` | 1 | 1 read | No | Medium | User authentication |
| `GetFollowers` | 1-2 | 1-2 reads | No | Medium | Cache lookup + optional DB read |
| `GetFollowees` | 1-2 | 1-2 reads | No | Medium | Cache lookup + optional DB read |
| `ReadPost` | 1-2 | 1-2 reads | No | Medium | Cache lookup + optional DB read |
| `ReadPosts` | 1-2 | 1-2 reads | No | Medium | Batch post retrieval |
| `ComposeText` | 2 | 0-2 writes | No | Medium | Text parsing + URL shortening + mention extraction |
| `ComposeUrls` | 1 | 0-1 writes | No | Medium | URL shortening |
| `ComposeUserMentions` | 1-2 | 1-2 reads | No | Medium | Username to ID conversion |

#### High Complexity Operations (5+ Service Calls)

| Operation | Service Calls | Database Ops | Concurrent? | Complexity | Notes |
|----------|--------------|--------------|-------------|-----------|-------|
| `ComposePost` | 7-8 | 3-5 writes | Yes (2 phases) | Very High | Most complex operation in the system |

**ComposePost Breakdown:**

**Phase 1: Concurrent Preparation (4 parallel calls):**
1. `ComposeText(text)` - Parse text, extract URLs and mentions
   - Calls `UrlShortenService.ComposeUrls()` (may write to DB)
   - Calls `UserMentionService.ComposeUserMentions()` (reads user DB)
2. `ComposeMedia(mediaTypes, mediaIDs)` - Create media objects (in-memory)
3. `ComposeUniqueId(postType)` - Generate post ID (in-memory)
4. `ComposeCreatorWithUserId(userID, username)` - Create creator object (may read user DB)

**Phase 2: Concurrent Storage (3 parallel calls):**
1. `StorePost(post)` - Persist post (writes to cache + DB)
2. `WriteUserTimeline(postID, userID)` - Add to user's timeline (writes to cache + DB)
3. `WriteHomeTimeline(postID, userID, userMentionIDs)` - Add to followers' timelines
   - Calls `GetFollowers(userID)` (reads social graph)
   - Writes to cache for: user + all followers + all mentioned users
   - **Fan-out**: O(1 + followers + mentions) cache writes

**Complexity Factors:**
- **7-8 service calls** across multiple services
- **Two-phase concurrent execution** with synchronization points
- **Fan-out writes**: Home timeline updates affect potentially hundreds of users
- **Multiple data dependencies**: Text, media, user, social graph
- **Cache + DB operations**: Dual-write pattern for performance
- **Error handling**: Must handle partial failures across 7+ operations

**WriteHomeTimeline Fan-out Analysis:**
- Retrieves all followers of the post author
- Adds post to home timeline of:
  - The author themselves
  - All followers (potentially 100s-1000s)
  - All mentioned users
- Each timeline update requires a cache read + write
- **Complexity scales with user's follower count**

---

## Comparison Summary

### Operation Count Distribution

| Complexity Level | SockShop | DSB_SN |
|----------------|----------|--------|
| Simple (1-2 calls) | 11 operations | 4 operations |
| Medium (2-4 calls) | 6 operations | 12 operations |
| High (5+ calls) | 1 operation | 1 operation |

### Most Complex Operations

**SockShop - NewOrder:**
- 5 service calls
- 4 concurrent + 1 sequential phase
- 6 database operations
- **Complexity Score: 5/10**

**DSB_SN - ComposePost:**
- 7-8 service calls
- 2 concurrent phases (4 + 3 operations)
- 3-5 database operations + O(N) cache operations
- **Complexity Score: 9/10**

### Key Differences

1. **Fan-out Complexity:**
   - **SockShop**: Operations affect single entities (one user, one cart, one order)
   - **DSB_SN**: Operations can affect hundreds of users (home timeline updates)

2. **Concurrency Patterns:**
   - **SockShop**: Simple parallel data fetching (4 concurrent reads)
   - **DSB_SN**: Multi-phase concurrency with complex dependencies

3. **Caching Strategy:**
   - **SockShop**: Minimal caching, mostly direct DB operations
   - **DSB_SN**: Aggressive caching with cache-aside pattern, dual-write complexity

4. **Data Relationships:**
   - **SockShop**: Simple relational data (user → cart → order)
   - **DSB_SN**: Complex graph relationships (social graph, timelines, mentions)

5. **Write Amplification:**
   - **SockShop**: 1 write per operation typically
   - **DSB_SN**: O(N) writes for timeline operations where N = followers + mentions

---

## Recommendations for Testing

### SockShop
- Focus on **NewOrder** as the primary stress test
- Test cart merge scenarios during login/registration
- Validate concurrent order placement

### DSB_SN
- **ComposePost** is the critical path - highest complexity
- Test with users having varying follower counts (10, 100, 1000+)
- Stress test home timeline reads under high write load
- Monitor cache hit rates and DB fallback behavior

---

## Complexity Scoring Methodology

Operations are scored based on:
1. **Service Call Count**: Base complexity
2. **Concurrency**: Parallel operations reduce perceived latency but increase coordination complexity
3. **Fan-out**: Operations affecting multiple entities increase complexity
4. **Data Dependencies**: Cross-service dependencies add complexity
5. **Error Handling**: More services = more failure modes

**Scoring:**
- **1-3**: Simple, single-service operations
- **4-6**: Medium complexity, multiple services, some concurrency
- **7-10**: High complexity, many services, complex concurrency, fan-out effects

