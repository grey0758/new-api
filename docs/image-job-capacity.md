# Async image job capacity

The opt-in image job ledger retains its idempotent admission, token ownership,
single execution claim, result-before-billing checkpoint and no replay on owner
loss. Capacity is now configured once at startup. Invalid, zero, negative,
noninteger or out-of-range values prevent executor startup.

| Environment variable | Default | Allowed |
| --- | ---: | ---: |
| IMAGE_JOBS_MAX_PENDING | 48 | 1–10000 |
| IMAGE_JOBS_MAX_PENDING_PER_USER | 8 | 1–10000, at most global pending |
| IMAGE_JOBS_WORKERS_STANDARD | 4 | 1–512 |
| IMAGE_JOBS_WORKERS_4K | 1 | 1–64 |
| IMAGE_JOBS_MAX_REQUEST_BYTES_TOTAL | 134217728 | 8 MiB–8 GiB |
| IMAGE_JOBS_MAX_RESULT_BYTES_TOTAL | 1073741824 | 48 MiB–64 GiB |
| IMAGE_JOBS_MAX_RETAINED | 10000 | 1–3000000 |

Pending includes queued and running jobs. The sum of workers must not exceed
pending capacity. The 8 MiB per-request and 48 MiB per-result bounds, 15-minute
queue/execution deadlines and seven-day tombstones remain. Expired result bodies
are cleared by existing maintenance; submitted unresolved jobs are not deleted.
Aggregate result bytes remain an admission guard, not a reservation or absolute
on-disk ceiling: executing jobs may checkpoint after the guard was checked.

At a capacity limit the API keeps HTTP 429 / `image_job_queue_full` and
`Retry-After: 5`. `X-Image-Queue-Limit` and a content-free diagnostic report
`global_pending`, `user_pending`, `request_bytes`, `result_bytes` or
`retained_jobs`. Idempotent receipt lookup precedes capacity checks.

The sgp011 deployment uses 1024 global/user pending, 128 standard + 8 fourK
workers, 1 GiB queued request bytes, 16 GiB stored result admission and 2.5 million
retained rows. The 200-request burst test verifies admission and the blocked
worker test verifies 136 distinct simultaneous claims. Neither sends paid
provider traffic nor proves 200 real Google completions per minute. Upstream
account concurrency, generation time, rate limits and quota still govern output.

Use a fresh write fence and drain both synchronous requests and queued/running
image jobs before changing the process. Keep the current database, results,
billing and auth on rollback. Do not clear the ledger to regain capacity.
