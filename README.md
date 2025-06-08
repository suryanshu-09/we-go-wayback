# We Go Wayback

A Golang rewrite of [wayback-discover-diff](https://github.com/internetarchive/wayback-discover-diff.git).

This app calculates and retrieves [Simhash](https://en.wikipedia.org/wiki/SimHash) values for archived web captures via the Wayback Machine.

**Tech Stack**:

- [Chi](https://github.com/go-chi/chi) – HTTP routing
- [Asynq](https://github.com/hibiken/asynq) – Background jobs
- [Redis](https://redis.io) – Storage and job backend

---

## 🚀 Getting Started

```bash
git clone https://github.com/yourname/we-go-wayback.git
cd we-go-wayback
./we-go-wayback
```

Run tests with

```
go test -v ./tests
```

**Requirements**:

- Go 1.20+
- Redis:6.0 running locally or via config

---

## 🧭 HTTP API

### `GET /`

Returns the current version.

---

### `GET /calculate-simhash?url={URL}&year={YEAR}`

Checks if a simhash calculation task exists for the URL/year.

- If a task is running:

```json
{ "status": "PENDING", "job_id": "xx-yy-zz" }
```

- If not, starts a new task:

```json
{ "status": "started", "job_id": "xx-yy-zz" }
```

---

### `GET /simhash?url={URL}&timestamp={TIMESTAMP}`

Returns the simhash for a specific capture.

- If found:

```json
{ "simhash": "XXXX" }
```

- If no captures:

```json
{ "status": "error", "message": "NO_CAPTURES" }
```

- If timestamp not found:

```json
{ "status": "error", "message": "CAPTURE_NOT_FOUND" }
```

---

### `GET /simhash?url={URL}&year={YEAR}`

Returns all calculated simhashes for the URL/year.

- If complete:

```json
{
  "captures": [["TIMESTAMP", "SIMHASH"], ...],
  "total": 123,
  "status": "COMPLETE"
}
```

- If pending:

```json
{
  "captures": [["TIMESTAMP", "SIMHASH"], ...],
  "total": 123,
  "status": "PENDING"
}
```

- If not captured:

```json
{ "status": "error", "message": "NOT_CAPTURED" }
```

- If Wayback has no captures:

```json
{ "status": "error", "message": "NO_CAPTURES" }
```

---

### `GET /simhash?url={URL}&year={YEAR}&compress=1`

Returns a compact version of the same JSON data.

---

### `GET /simhash?url={URL}&year={YEAR}&page={PAGE}`

Returns paginated results (page size from `conf.yml`).

```json
[
  ["pages", NUMBER_OF_PAGES],
  ["TIMESTAMP", "SIMHASH"],
  ...
]
```

Note: SIMHASH values are base64 encoded.

---

### `GET /job?job_id={JOB_ID}`

Returns the status of a specific job.

```json
{
  "status": "pending",
  "job_id": "xx-yy-zz",
  "info": "X out of Y captures have been processed"
}
```

---

## ⚙️ Configuration

Edit `conf.yml` to set:

- Redis connection
- Snapshot/page limits
- Simhash TTL

---

## 🧪 Running

```bash
go run main.go
```

---

## 📜 License

MIT
