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

```bash
npm run d1:migrations
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

### GET /{type}

Get data from D1 database.

**Types**: `config`, `path_stats`, `banned_ips`

**Example**:
```bash
curl https://your-worker.workers.dev/config
```

**Response**:
```json
{
  "type": "config",
  "data": { ... },
  "updated_at": 1700000000000
}
```

### POST /{type}

Save data to D1 database.

**Types**: `config`, `path_stats`, `banned_ips`

**Example**:
```bash
curl -X POST https://your-worker.workers.dev/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-token" \
  -d '{"data": {"your": "config"}}'
```

**Response**:
```json
{
  "success": true,
  "type": "config",
  "updated_at": 1700000000000
}
```

## Database Schema

The D1 database contains three tables:

- `config` - Stores config.json
- `path_stats` - Stores path_stats.json
- `banned_ips` - Stores banned_ips.json

Each table has:
- `id` (INTEGER PRIMARY KEY) - Always 1 (single row per table)
- `data` (TEXT) - JSON string of the data
- `updated_at` (INTEGER) - Unix timestamp in milliseconds

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

Update your proxy-go configuration to use this Worker endpoint instead of S3:

```bash
# Set environment variables
export D1_SYNC_ENABLED=true
export D1_SYNC_URL=https://your-worker.workers.dev
export D1_SYNC_TOKEN=your-api-token  # Optional
```

The proxy-go server will automatically sync data to D1 instead of S3.
