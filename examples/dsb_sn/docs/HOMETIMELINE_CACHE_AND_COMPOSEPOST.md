# HomeTimeline service and cache behavior in the ComposePost workflow

## Where HomeTimeline fits in ComposePost

**ComposePost** (in `composepost_service`) does:

1. **Phase 1 (parallel):** ComposeText, ComposeMedia, ComposeUniqueId, ComposeCreatorWithUserId  
2. **Phase 2 (parallel):** StorePost, WriteUserTimeline, **WriteHomeTimeline**

So the only HomeTimeline call in the compose-post path is **WriteHomeTimeline**. There is no ReadHomeTimeline in this path.

---

## What WriteHomeTimeline does (cache writes only)

1. **GetFollowers(ctx, userID)**  
   Calls SocialGraphService to get the list of followers of the post author (`userID`).

2. **Recipients = followers + mentioned users**  
   Builds a set: every follower ID plus every `userMentionIDs` (users @-mentioned in the post). The post author is **not** added to this set in the current code (only followers + mentions).

3. **Redis path (when backend is Redis):**  
   **ZAddBatch(ctx, operations)**  
   - One operation per recipient: `(key = user ID as string, score = timestamp, member = post ID as string)`.  
   - So each recipient’s home timeline is a **sorted set** in Redis: key = `"<userID>"`, members = post IDs, scores = timestamps.  
   - One pipeline per ComposePost; many ZADD commands in that pipeline (one per recipient).

4. **Non-Redis fallback:**  
   For each recipient: **Get** that user’s key → append the new post → **Put** back (read-modify-write per user).

So in the **compose-post workflow**, the hometimeline cache is only **written** (ZAddBatch or Get+Put). No cache read is done in this path when using Redis.

---

## What ReadHomeTimeline does (cache reads)

Used when something (e.g. API or a “read feed” workload) asks for a user’s home timeline. **Not** called by the compose-post.lua script.

1. **Redis path:**  
   **ZRevRange(ctx, userIDStr, start, stop-1)**  
   - One cache **read** from the sorted set for that user.  
   - Returns post IDs in reverse order by score (newest first).  
   - Then **ReadPosts** is called to load full post content from PostStorage.

2. **Non-Redis fallback:**  
   **Get(ctx, userIDStr, &postInfos)** → slice by start/stop → **ReadPosts**.

So **ReadHomeTimeline** is the only path that **reads** from the hometimeline cache.

---

## Compose-post load test (compose-post.lua)

The script `scripts/compose-post.lua` only issues **POST /ComposePost**. It does **not** call ReadHomeTimeline or any “read feed” endpoint.

So when you run a compose-post load test:

- **ComposePost** is invoked at the requested RPS.
- Each ComposePost triggers **WriteHomeTimeline** once.
- WriteHomeTimeline does **only cache writes** to the hometimeline Redis:
  - One **ZAddBatch** per ComposePost (many ZADDs in one pipeline, one per recipient).
- **No** ReadHomeTimeline, hence **no** cache reads from the hometimeline cache during this workload.

Cache keys are **user IDs as strings** (e.g. `"42"`, `"1001"`). Each key is a sorted set; members are post IDs (strings), scores are timestamps. No TTL is set (your Redis cache plugin uses `Set(..., 0)` and ZAdd without expiry).

---

## Summary

| Trigger                    | HomeTimeline call        | Cache operation (Redis)                          |
|---------------------------|--------------------------|--------------------------------------------------|
| **ComposePost** (compose-post.lua) | WriteHomeTimeline only   | **Writes only:** ZAddBatch (many ZADD per post) |
| **Read feed** (e.g. /ReadHomeTimeline) | ReadHomeTimeline         | **Read:** ZRevRange for that user, then ReadPosts |

So the part of the compose-post workflow that drives the hometimeline cache is **WriteHomeTimeline**: it updates the sorted set for each recipient (followers + mentioned users) by adding the new post ID with a timestamp score.
