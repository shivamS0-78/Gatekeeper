# Gatekeeper

A high-performance, resilient API Gateway and Rate Limiter written in Go. Gatekeeper protects backend services from traffic spikes and abuse using a Redis-backed rate-limiting algorithm, client-aware rule caching, reverse proxying, dynamic health checking, and automatic failover.

---

## Features

- **Sliding Window Rate Limiting**: Accurate, burst-resistant rate limiting implemented using Redis Sorted Sets (`ZREMRANGEBYSCORE`, `ZADD`, `ZCARD`) via atomic pipelines.
- **Client & IP-Based Identification**: Identifies clients using `X-User-ID` headers with fallback to remote client IP addresses (`RemoteAddr`).
- **In-Memory Rule Cache**: Thread-safe (`sync.RWMutex`) rule caching layer with TTL expiration to minimize database lookups.
- **Reverse Proxy & Routing**:
  - Declarative YAML routing table.
  - Path prefix stripping (`strip_prefix: true`).
  - Automatic hop-by-hop header cleanup (`Connection`, `Keep-Alive`, `Upgrade`, etc.).
  - Forwarded headers management (`X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`).
- **Load Balancing & Failover**:
  - Atomic round-robin upstream server selection.
  - Configurable retry attempts on transient network failures or retryable HTTP status codes (`502`, `503`, `504`).
- **Active Health Checking**: Background ticker periodically validates upstream `/health` endpoints and marks unhealthy nodes out of rotation.
- **Standardized Rate Limit Headers**: Injects rate limit headers into every downstream response:
  - `X-Rate-Limit-Limit`
  - `X-Rate-Limit-Remaining`
  - `X-Rate-Limit-Reset`
- **Graceful Shutdown**: Handles `SIGINT` and `SIGTERM` signals for clean HTTP server teardown with configurable context timeouts.

---

## Architecture

<img width="2816" height="1536" alt="Gemini_Generated_Image_1wxr3m1wxr3m1wxr" src="https://github.com/user-attachments/assets/945fc16e-da27-409a-8b09-709ab139024f" />


## Project Structure

```
Gatekeeper/
├── backend/
│   └── backend.go               # Mock upstream service for local development & testing
├── cmd/
│   └── gatekeeper/
│       └── main.go              # Gateway server entrypoint & graceful shutdown
├── internal/
│   ├── config/
│   │   ├── config.go            # Config loading and schema definition
│   │   └── config.yaml          # Gateway routes, upstreams, and port configurations
│   ├── database/
│   │   └── db.go                # PostgreSQL & Redis connection initializers
│   ├── gateway/
│   │   └── gateway.go           # Core HTTP handler, load balancing, & health checks
│   ├── proxy/
│   │   └── reverse_proxy.go     # Reverse proxy logic, header filtering, and status checks
│   └── ratelimiter/
│       ├── cache.go             # In-memory rule cache with expiration TTL
│       ├── limiter.go           # Redis sliding-window algorithm implementation
│       └── middleware.go        # Standalone HTTP rate-limiting middleware
├── go.mod
├── go.sum
└── README.md
```

---

## Configuration

Gatekeeper is configured via a YAML file (`internal/config/config.yaml`):

```yaml
server:
  port: 8080

routes:
  - path: /api/users
    upstreams:
      - http://localhost:9000
      - http://localhost:9010
      - http://localhost:9020
    strip_prefix: true
    retries: 2

  - path: /api/orders
    upstreams:
      - http://localhost:9001
      - http://localhost:9011
    strip_prefix: true
    retries: 2
```

### Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `CONFIG_PATH` | Path to the YAML configuration file | `internal/config/config.yaml` |
| `REDIS_ADDR` | Redis server network address | `localhost:6379` |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://postgres:password@localhost:5432/ratelimiter?sslmode=disable` |

---

## Getting Started

### Prerequisites

- [Go](https://go.dev/dl/) 1.22+
- [Redis](https://redis.io/) (running locally or via Docker)

### 1. Start Redis

```bash
docker run -d --name gatekeeper-redis -p 6379:6379 redis:alpine
```

### 2. Start Mock Upstream Backend

Run the included mock backend server on port `9000`:

```bash
go run backend/backend.go
```

### 3. Launch Gatekeeper

```bash
go run cmd/gatekeeper/main.go
```

The gateway will start listening on port `8080`.

---

## Testing

### Forward a Request

```bash
curl -i http://localhost:8080/api/users \
  -H "X-User-ID: user_123"
```

**Example Response Headers:**

```http
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
X-Rate-Limit-Limit: 5
X-Rate-Limit-Remaining: 4
X-Rate-Limit-Reset: 1727292650

Hello from backend! 
Method : GET
Path : /
```

### Rate Limit Exceeded

When the threshold is exceeded within the active window:

```bash
# Sending excessive rapid requests:
curl -i http://localhost:8080/api/users -H "X-User-ID: user_123"
```

**Response:**

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
X-Rate-Limit-Limit: 5
X-Rate-Limit-Remaining: 0
X-Rate-Limit-Reset: 1727292660

{"error":"rate limit exceeded"}
```

---

## License

This project is licensed under the MIT License.
