# Event Collector: Disk-First Storage Pipeline

## 1. Overview

The event collector uses a **disk-first storage pipeline**: all events received via `POST /v1/events` are immediately appended to local JSONL files, which are periodically rotated, optionally compressed, and uploaded to remote storage. There is no in-memory buffering path — every event hits disk before it leaves the collector.

The `--event-compression-enabled` flag controls **only whether rotated files are gzipped before upload**. It does not affect the write path, rotation logic, or disk pressure enforcement.

### Key Design Decisions

- Events are always written to local disk as JSONL, regardless of compression setting.
- Disk pressure backpressure (503) is always active.
- Rotation is triggered by time, file size, session change, node ID change, or graceful shutdown.
- No in-memory event buffer exists; the `--event-flush-interval` flag has been removed.

---

## 2. Architecture

### 2.1 Data Flow

```
POST /v1/events (PersistEvents)
       │
       ▼
  Disk pressure check (≥ 80% of max → 503 + Retry-After: 10)
       │
       ▼
  Validate each event (timestamp, sessionName present & parseable)
       │
       ▼
  Session change? → rotateAllFilesLocked()
       │
       ▼
  Enrich event with _nodeId → categorize → get/create active JSONL file
       │
       ▼
  Append JSON line + newline → bufio.Writer (64 KB)
       │
       ▼
  Flush touched writers → return 200 OK
       │
  ─ ─ ─ ─ ─ ─ ─ ─ (async) ─ ─ ─ ─ ─ ─ ─ ─
       │
  ┌─ Rotation trigger fires ─────────────────┐
  │  time / size / session / nodeID / shutdown │
  └───────────────────────────────────────────┘
       │
       ▼
  rotateFileLocked():
    1. Flush + Sync + Close file
    2. Empty file → delete locally, skip upload
    3. Non-empty → build rotationTask → enqueue to rotationQueue (cap 256)
       │
       ▼
  compressionUploadWorker() drains rotationQueue:
    • compressionEnabled = true  → gzipFile() → upload .jsonl.gz → delete both local files
    • compressionEnabled = false → upload .jsonl directly → delete local file
```

### 2.2 Goroutine Model

| Goroutine | Lifecycle | Responsibility |
|-----------|-----------|----------------|
| `rotationLoop` | Runs for collector lifetime | Checks rotation conditions every 30 s via `checkRotation()` |
| `compressionUploadWorker` | Runs for collector lifetime | Drains `rotationQueue`, optionally compresses, uploads, cleans up |
| `diskReconcileLoop` | Runs for collector lifetime | Reconciles `totalDiskUsed` counter with actual directory walk every 60 s |
| `watchNodeIDFile` | Runs for collector lifetime | Watches `raylet_node_id` via fsnotify; rotates all files on content change |
| HTTP server | Runs for collector lifetime | Serves `POST /v1/events` |

All goroutines are joined via `workersWG` on graceful shutdown after `stop` is closed.

### 2.3 Rotation Triggers

Five conditions trigger file rotation (close current JSONL → enqueue for upload):

| # | Trigger | Condition | Source |
|---|---------|-----------|--------|
| 1 | **Time-based** | File age ≥ `--event-rotation-interval` (default 5 min) | `rotationLoop` → `checkRotation()` |
| 2 | **Size-based** | File size ≥ `--event-max-file-size-mb` (default 100 MB) | Same check, `||` with time |
| 3 | **Session change** | Incoming `sessionName` ≠ current session | `PersistEvents()` — synchronous |
| 4 | **Node ID change** | `raylet_node_id` file content changes | `watchNodeIDFile()` via fsnotify |
| 5 | **Graceful shutdown** | `stop` channel closed | `Run()` → `rotateAllFiles()` |

When the rotation queue is full (256 tasks), a retry goroutine sleeps 5 s and re-enqueues.

### 2.4 Disk Pressure Backpressure

When `totalDiskUsed ≥ --event-max-disk-mb × 0.8` (watermark), `POST /v1/events` returns:

```
HTTP/1.1 503 Service Unavailable
Retry-After: 10
```

The `totalDiskUsed` counter is updated on every write and periodically reconciled against a real directory walk by `diskReconcileLoop`.

### 2.5 Startup Resume

On startup, `resumePendingFiles()` walks the data directory for leftover files from a prior run:

| File suffix | Action |
|-------------|--------|
| `.jsonl` | Enqueue to `rotationQueue` for compress + upload |
| `.jsonl.gz` | Upload directly via `uploadOnly()` (no re-compression) |

---

## 3. Configuration

### 3.1 Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--event-compression-enabled` | `false` | Enable gzip compression when uploading rotated JSONL files to remote storage. When `false`, plain JSONL files are uploaded as-is. |
| `--event-rotation-interval` | `5m` | Time threshold to rotate active JSONL file |
| `--event-max-file-size-mb` | `100` | Size threshold (MB) to rotate active JSONL file |
| `--event-max-disk-mb` | `200` | Max total disk usage (MB) before 503 backpressure |

### 3.2 Environment Variables

| Variable | Overrides |
|----------|-----------|
| `RAY_COLLECTOR_EVENT_COMPRESSION_ENABLED` | `--event-compression-enabled` |
| `RAY_COLLECTOR_EVENT_ROTATION_INTERVAL` | `--event-rotation-interval` |
| `RAY_COLLECTOR_EVENT_MAX_FILE_SIZE_MB` | `--event-max-file-size-mb` |
| `RAY_COLLECTOR_EVENT_MAX_DISK_MB` | `--event-max-disk-mb` |

### 3.3 Behavior by Compression Mode

| Dimension | `compressionEnabled = true` | `compressionEnabled = false` |
|-----------|-----------------------------|------------------------------|
| Local write | JSONL to disk | JSONL to disk *(same)* |
| Rotation | By time / size / session / nodeID / shutdown | *(same)* |
| Upload payload | `.jsonl.gz` (gzip compressed) | `.jsonl` (plain text) |
| Disk pressure 503 | Active | Active *(same)* |
| Local cleanup after upload | Delete `.jsonl` + `.jsonl.gz` | Delete `.jsonl` |

---

## 4. Remote Storage Layout

### 4.1 Key Format

```
{root}/{cluster}_{namespace}/{session}/{category}/{nodeID}-{hour}-{unixNano}{ext}
```

| Component | Example |
|-----------|---------|
| `root` | `history` |
| `cluster_namespace` | `raycluster_default` |
| `session` | `session_2026-05-25_23-57-32_105179_1` |
| `category` | `node_events` or `job_events/{jobID}` |
| `nodeID` | `e0b640edbdb2c12b7b10237e04d0f305688d204419d23515e9eb5eda` |
| `hour` | `2026-05-26-07` |
| `unixNano` | `1779779262997409971` |
| `ext` | `.jsonl.gz` (compressed) or `.jsonl` (uncompressed) |

### 4.2 Example Directory Tree

```
history/raycluster_default/session_2026-05-25_23-57-32_105179_1/
├── node_events/
│   ├── e0b640ed...-2026-05-26-06-1779778962997018939.jsonl
│   └── e0b640ed...-2026-05-26-07-1779779262997409971.jsonl
├── job_events/
│   └── AQAAAA==/
│       ├── 2bf074fd...-2026-05-26-07-1779778993450158374.jsonl
│       └── e0b640ed...-2026-05-26-07-1779779262997550252.jsonl
└── logs/
    └── {nodeID}/events/
        ├── event_CORE_WORKER_1044.log
        └── event_RAYLET.log
```

### 4.3 Event Categorization

| Event Type | Category |
|------------|----------|
| `NODE_LIFECYCLE_EVENT`, `NODE_DEFINITION_EVENT` | `node_events` |
| Events with a `jobId` (task, actor, driver job lifecycle) | `job_events/{jobID}` |
| Fallback (unrecognized type) | `node_events` |

---

## 5. End-to-End Verification

The following evidence was collected on a remote Kubernetes cluster to validate the full pipeline: collector → OSS storage → historyserver query.

### 5.1 Environment

| Item | Value |
|------|-------|
| Cluster | Remote Kubernetes cluster |
| RayCluster CR | `raycluster-historyserver` (namespace: default) |
| Collector image | `yueming-acr-registry.cn-hongkong.cr.aliyuncs.com/kuberay/collector:latest` |
| HistoryServer image | `yueming-acr-registry.cn-hongkong.cr.aliyuncs.com/kuberay/historyserver:latest` |

### 5.2 Collector Upload

- Head and worker collector sidecars started normally.
- Head collector uploaded node events (4 events) and job events (15 events).
- Worker collector uploaded job events (10 events).
- Storage path: `log/raycluster-historyserver_default/session_xxx/{category}/...`

<details>
<summary>OSS file structure (from HistoryServer ListFiles logs)</summary>

```
log/raycluster-historyserver_default/session_2026-05-25_23-57-32_105179_1/
├── logs/
│   ├── 2bf074fd.../events/  (5 files: event_CORE_WORKER_*, event_RAYLET)
│   └── e0b640ed.../events/  (5 files: event_AUTOSCALER, event_CORE_WORKER_*, event_GCS, event_RAYLET)
├── job_events/
│   └── AQAAAA==/  (3 files)
└── node_events/  (3 files)
```

</details>

### 5.3 Rotation Interval

With the default 5-minute rotation interval, the observed upload cadence matched precisely:

| Event Type | Timestamp Delta | Actual Interval |
|------------|-----------------|-----------------|
| node_events | `1779779262997409971 − 1779778962997018939` | 300.000391 s ≈ 5 min |
| job_events | Two consecutive files | ~4.5 min (within one cycle) |

### 5.4 HistoryServer Live Query

| API Endpoint | Result |
|--------------|--------|
| `/clusters/` | Session list returned |
| `/api/v0/tasks` | 3 `process_data` tasks, all FINISHED |
| `/nodes?view=summary` | 2 nodes (head + worker) |
| `/events` | AUTOSCALER events |

### 5.5 Historical Session Persistence

After deleting the RayCluster CR, the historyserver continued serving all historical data:

- `/clusters/` listed 3 historical sessions.
- Tasks, nodes, and events for `session_2026-05-25_23-57-32_105179_1` were fully preserved.

<details>
<summary>/clusters/ API response</summary>

```json
[
  {"name": "raycluster-historyserver", "namespace": "default", "sessionName": "session_2026-05-26_00-14-02_584855_1", "createTime": "2026-05-26T00:14:02Z"},
  {"name": "raycluster-historyserver", "namespace": "default", "sessionName": "session_2026-05-25_23-57-32_105179_1", "createTime": "2026-05-25T23:57:32Z"},
  {"name": "raycluster-historyserver", "namespace": "default", "sessionName": "session_2026-05-18_01-06-59_714721_1", "createTime": "2026-05-18T01:06:59Z"}
]
```

</details>

### 5.6 Ray Task Execution

Three parallel `process_data` tasks submitted from the head pod, executed on worker nodes:

| Task | Result |
|------|--------|
| Batch 0 | 380 |
| Batch 1 | 800 |
| Batch 2 | 1220 |

---

## 6. Bug Fix: Aliyun OSS Path Construction

During verification, a bug was discovered in the Aliyun OSS storage implementation:

| Issue | Fix |
|-------|-----|
| `GetContent` method was missing `OssRootDir` and `clusterId` prefix in path construction | `ray.go` changed to `path.Join(r.OssRootDir, clusterId, fileName)` |

<details>
<summary>Before fix — ListFiles vs GetObject path mismatch</summary>

```
# ListFiles succeeds (has log/ prefix)
"[ListFiles]Returned objects in log/raycluster-historyserver_default/session_.../logs/..."

# GetObject fails (missing log/ prefix → 404)
"Prepare to get object session_2026-05-25_.../logs/... → 404 NoSuchKey"

# Session load fails
"Failed to load session: read 0 of 4 event files: likely transient storage outage"
```

</details>

<details>
<summary>After fix — paths consistent</summary>

```
# GetObject succeeds (has log/ prefix)
"Prepare to get object log/raycluster-historyserver_default/session_.../logs/.../event_CORE_WORKER_1044.log"
# Event loading succeeds
```

</details>

---

## 7. Implementation Details

### 7.1 Files Modified

| File | Changes |
|------|---------|
| `pkg/collector/eventcollector/eventcollector.go` | Removed 7 functions, `memEvent` struct, `flushInterval`/`memEvents` fields. Simplified `Run()`, `PersistEvents()`, `watchNodeIDFile()` to single disk-only path. |
| `pkg/collector/types/types.go` | Removed `EventFlushInterval` field. Updated `EventCompressionEnabled` documentation. |
| `cmd/collector/main.go` | Removed `eventFlushInterval` variable, flag, env override, config field. Updated flag description. |
| `pkg/collector/eventcollector/eventcollector_test.go` | Removed 7 memory-buffer tests. Retained 18 disk-pipeline tests. |

**Net change:** `4 files changed, 36 insertions(+), 503 deletions(−)`

### 7.2 Removed Functions

| Function | Former Purpose |
|----------|---------------|
| `persistEventsBuffered` | Append event to in-memory buffer |
| `periodicMemFlushLoop` | Timer-based memory flush goroutine |
| `flushAllMemEvents` | Drain memory buffer on shutdown |
| `partitionMemEventsForNodeChangeLocked` | Split buffer by nodeID on node change |
| `flushMemEventsInternal` | Bucket + concurrent upload from memory |
| `uploadMemBucket` | Serialize bucket to JSON array + upload (legacy format) |
| `buildLegacyEventStorageKey` | Legacy key format (no extension) |

### 7.3 Flag Compatibility

| Flag / Env Var | Status |
|----------------|--------|
| `--event-compression-enabled` | Retained; now controls only gzip on upload |
| `--event-flush-interval` | **Removed** |
| `RAY_COLLECTOR_EVENT_COMPRESSION_ENABLED` | Retained |
| `RAY_COLLECTOR_EVENT_FLUSH_INTERVAL` | **Removed** |

---

## 8. Unit Tests

| Command | Result |
|---------|--------|
| `go build ./...` | Pass |
| `go vet ./...` | Pass |
| `go test ./pkg/collector/eventcollector/` | 18/18 pass |
| `go test ./pkg/eventserver/...` | All pass |

Retained tests cover: helper functions, gzip compression, disk accounting, JSONL file creation, rotation (empty file drop, size trigger, session change), disk pressure rejection, end-to-end persist + upload (compressed and uncompressed), storage key format, and atomic counter types.
