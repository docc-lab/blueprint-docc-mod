# Redis cache continuous CPU usage

## Symptom

The hometimeline (and other) Redis cache pods show ~1 core CPU usage in `kubectl top pods` even minutes after load tests have ended.

## Likely causes

The cache containers use the stock **redis:latest** image with **no custom config**. So Redis runs with default persistence and server settings.

### 1. RDB snapshotting (most likely)

Default Redis uses:

- `save 900 1`   — save if ≥1 key changed in 15 min  
- `save 300 10`  — save if ≥10 keys changed in 5 min  
- `save 60 10000` — save if ≥10000 keys changed in 1 min  

After a load test the dataset can be large (many keys). When the last window had ≥10000 writes, Redis starts **BGSAVE** every 60 seconds. A single BGSAVE on a large dataset can use a full core for tens of seconds or longer. So you see sustained CPU until the snapshot finishes, and it can repeat if conditions are met again.

### 2. AOF (if enabled in your image)

If the image or a mounted config has `appendonly yes`, Redis may run **AOF rewrites** in the background after a lot of writes. Rewrites are CPU-heavy and can run for a long time, looking like continuous CPU.

### 3. Key expiration (only if you set TTLs)

Your app currently uses `Set(..., 0)` (no TTL) and does not set expiry on sorted-set keys. So **active expiration is not** the cause with the current code. If you later add TTLs, Redis’s active-expire cycle (controlled by `hz`) can add CPU when many keys expire.

## How to verify

Exec into the Redis cache pod and run:

```bash
# Persistence and current background work
redis-cli INFO persistence

# Whether a save/rewrite is in progress (e.g. rdb_bgsave_in_progress:1)
redis-cli INFO persistence | grep -E "rdb_|aof_"

# Save rules
redis-cli CONFIG GET save

# AOF enabled?
redis-cli CONFIG GET appendonly

# Approximate key count (large = long BGSAVE)
redis-cli DBSIZE

# Commands per second (should be near 0 when “idle”)
redis-cli INFO stats
```

If you see `rdb_bgsave_in_progress:1` or a large `DBSIZE` and recent `rdb_last_save_time`, RDB is the culprit. If `aof_rewrite_in_progress:1`, AOF rewrite is.

## Mitigation: disable persistence for caches

These Redis instances are used only as **caches**. You can avoid RDB and AOF entirely so there is no background save or rewrite:

- **RDB:** disable with `save ""` (or `--save ""`).
- **AOF:** ensure `appendonly no` (default).

Then Redis will not run BGSAVE or AOF rewrite, and CPU should drop to near zero when there is no traffic.

### Option A: Override command in the deployment

In the generated K8s deployment (e.g. `build_nt4/k8s/hometimeline-cache-nt4-ctr-deployment.yaml`), add a `command` (and optionally `args`) to the container so the process is:

```yaml
command: ["redis-server"]
args: ["--save", "", "--appendonly", "no"]
```

Regenerated deployments will overwrite this unless the generator is changed (see Option B).

### Option B: Change the Redis plugin to emit a cache-optimized command

In `plugins/redis/` you could make the generated container spec use:

- `redis-server --save "" --appendonly no`

so every Redis cache container (hometimeline, user, post, social, usertimeline) starts with persistence disabled. That is the right approach if all such containers are caches and you never need to persist them.

### Option C: ConfigMap + custom redis.conf

Mount a `redis.conf` with `save ""` and `appendonly no` and point the container at it. More flexible but more moving parts than Option B.

## Summary

| Cause           | Check                                      | Mitigation                          |
|----------------|---------------------------------------------|-------------------------------------|
| RDB BGSAVE     | `INFO persistence`, `CONFIG GET save`, DBSIZE | `--save ""` or `save ""` in config  |
| AOF rewrite    | `INFO persistence`, `CONFIG GET appendonly`  | `--appendonly no`                    |
| Key expiration | Only if app sets TTLs                        | Lower `hz` if you add TTLs later    |

Disabling persistence for cache-only Redis (Option B or A) is the most direct fix for continuous CPU after load.
