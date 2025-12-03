# Proxy-Go Sync Worker

Cloudflare Worker for synchronizing proxy-go configuration and statistics data to D1 database.

## Features

- Store config.json, path_stats.json, and banned_ips.json in D1 database
- RESTful API for GET/POST operations
- CORS support for cross-origin requests
- Optional API token authentication
- Automatic timestamp tracking

## Setup

### 1. Install Dependencies

```bash
cd cloudflare-worker
npm install
```

### 2. Create D1 Database

```bash
npm run d1:create
```

This will output a database ID. Copy it and paste into `wrangler.toml`:

```toml
[[d1_databases]]
binding = "DB"
database_name = "proxy-go-data"
database_id = "your-database-id-here"
```

### 3. Run Migrations

Apply all migrations (initial schema + metrics tables):

```bash
npm run d1:migrations
```

Or apply them individually:

```bash
# Initial schema (config, path_stats, banned_ips)
wrangler d1 migrations apply proxy-go-data --file=migrations/0001_initial_schema.sql

# Metrics tables (status_codes, latency_distribution)
wrangler d1 migrations apply proxy-go-data --file=migrations/0002_add_metrics_tables.sql
```

### 4. (Optional) Set API Token

For production, add an API token for authentication:

```bash
wrangler secret put API_TOKEN
```

Or add it to `wrangler.toml` for development:

```toml
[vars]
API_TOKEN = "your-secure-token"
```

### 5. Deploy

```bash
npm run deploy
```

## API Endpoints

All endpoints support CORS and return JSON responses.

### Authentication

If `API_TOKEN` is set, include it in the `Authorization` header:

```
Authorization: Bearer your-api-token
```

### Path Stats API

#### `GET /path-stats?path=/xxx` - Get path statistics
```bash
curl https://your-worker.workers.dev/path-stats?path=/b2
```

Response:
```json
{
  "success": true,
  "data": [
    {
      "path": "/b2",
      "request_count": 12345,
      "error_count": 10,
      "bytes_transferred": 1073741824,
      "status_2xx": 12000,
      "status_3xx": 200,
      "status_4xx": 100,
      "status_5xx": 45,
      "cache_hits": 8000,
      "cache_misses": 4345,
      "cache_hit_rate": 0.65,
      "bytes_saved": 536870912,
      "avg_latency": "45.23ms",
      "last_access_time": 1700000000,
      "updated_at": 1700000000
    }
  ]
}
```

#### `POST /path-stats` - Batch update path statistics
```bash
curl -X POST https://your-worker.workers.dev/path-stats \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{
    "stats": [
      {
        "path": "/b2",
        "request_count": 12345,
        "error_count": 10,
        ...
      }
    ]
  }'
```

### Banned IPs API

#### `GET /banned-ips?active=true` - Get banned IPs
```bash
curl https://your-worker.workers.dev/banned-ips?active=true
```

#### `POST /banned-ips` - Batch update banned IPs
```bash
curl -X POST https://your-worker.workers.dev/banned-ips \
  -H "Content-Type: application/json" \
  -d '{"bans": [...]}'
```

#### `GET /banned-ips/history?ip=xxx&limit=100` - Get ban history
```bash
curl https://your-worker.workers.dev/banned-ips/history?ip=192.168.1.1&limit=50
```

### Config Maps API

#### `GET /config-maps?enabled=true` - Get all path configurations
```bash
curl https://your-worker.workers.dev/config-maps?enabled=true
```

#### `GET /config-maps/{path}` - Get specific path config
```bash
curl https://your-worker.workers.dev/config-maps/%2Fb2
```

#### `POST /config-maps` - Batch update path configs
```bash
curl -X POST https://your-worker.workers.dev/config-maps \
  -H "Content-Type: application/json" \
  -d '{"maps": [...]}'
```

#### `DELETE /config-maps/{path}` - Delete path config
```bash
curl -X DELETE https://your-worker.workers.dev/config-maps/%2Fb2
```

### Config Other API

#### `GET /config-other?key=compression` - Get system configs
```bash
curl https://your-worker.workers.dev/config-other?key=compression
```

#### `POST /config-other` - Batch update system configs
```bash
curl -X POST https://your-worker.workers.dev/config-other \
  -H "Content-Type: application/json" \
  -d '{"configs": [...]}'
```

### Metrics API (NEW)

#### `GET /metrics/status-codes` - Get HTTP status code statistics
```bash
curl https://your-worker.workers.dev/metrics/status-codes
```

Response:
```json
{
  "success": true,
  "data": [
    {
      "status_code": "200",
      "count": 12345,
      "updated_at": 1700000000
    },
    {
      "status_code": "404",
      "count": 123,
      "updated_at": 1700000000
    }
  ]
}
```

#### `POST /metrics/status-codes` - Batch update status code stats
```bash
curl -X POST https://your-worker.workers.dev/metrics/status-codes \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": [
      {"status_code": "200", "count": 12345, "updated_at": 1700000000},
      {"status_code": "404", "count": 123, "updated_at": 1700000000}
    ]
  }'
```

#### `GET /metrics/latency` - Get latency distribution
```bash
curl https://your-worker.workers.dev/metrics/latency
```

Response:
```json
{
  "success": true,
  "data": [
    {"bucket": "lt10ms", "count": 5000, "updated_at": 1700000000},
    {"bucket": "10-50ms", "count": 3000, "updated_at": 1700000000},
    {"bucket": "50-200ms", "count": 1500, "updated_at": 1700000000},
    {"bucket": "200-1000ms", "count": 300, "updated_at": 1700000000},
    {"bucket": "gt1s", "count": 50, "updated_at": 1700000000}
  ]
}
```

#### `POST /metrics/latency` - Batch update latency distribution
```bash
curl -X POST https://your-worker.workers.dev/metrics/latency \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": [
      {"bucket": "lt10ms", "count": 5000, "updated_at": 1700000000},
      ...
    ]
  }'
```

## Database Schema

The D1 database uses column-based storage with the following tables:

### 1. Path Stats (`path_stats`)
- Stores per-path request statistics
- Columns: path, request_count, error_count, bytes_transferred, status_2xx/3xx/4xx/5xx, cache_hits, cache_misses, cache_hit_rate, bytes_saved, avg_latency, last_access_time, updated_at

### 2. Banned IPs (`banned_ips`, `banned_ips_history`)
- Current bans and historical records
- Columns: ip, ban_time, ban_end_time, reason, error_count, is_active, unban_time, unban_reason, updated_at

### 3. Config Maps (`config_maps`)
- Path-based routing configurations
- Columns: path, default_target, enabled, extension_rules (JSON), cache_config (JSON), created_at, updated_at

### 4. Config Other (`config_other`)
- System-wide configuration (compression, security, cache, mirror_cache)
- Columns: key, value (JSON), description, updated_at

### 5. Metrics (`status_codes`, `latency_distribution`)
- HTTP status code statistics and latency buckets
- `status_codes`: status_code, count, updated_at
- `latency_distribution`: bucket (lt10ms, 10-50ms, 50-200ms, 200-1000ms, gt1s), count, updated_at

## Development

```bash
# Start local dev server
npm run dev

# View logs
npm run tail

# Run migrations locally
wrangler d1 migrations apply proxy-go-data --local
```

## Integration with Proxy-Go

Set these environment variables in your proxy-go server:

```bash
# D1 Sync Configuration
D1_SYNC_URL=https://your-worker.workers.dev
D1_SYNC_TOKEN=your-api-token  # Must match Worker's API_TOKEN
```

The proxy-go server will automatically:
- Download latest config from D1 on startup
- Sync config changes to D1 every 10 minutes
- Save metrics (status codes, latency) to D1 every 5 minutes
- Sync path stats and banned IPs to D1 every 10 minutes

**No S3 configuration needed** - all data is stored in D1 database.
