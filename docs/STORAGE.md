# Storage

Smart Proxy persists two things per request: **metadata** in SQLite (small, indexed, queryable from the dashboard) and **body data** (the request/response payloads plus their headers) in append-only JSONL files on a pluggable blob backend (local disk or any S3-compatible object store).

This document explains how both halves work, how to size retention so the disk doesn't fill up, and how to configure S3 against AWS, Hetzner Object Storage, MinIO, Cloudflare R2, or any other S3-compatible provider.

## Table of contents

- [Overview](#overview)
- [Metadata store (SQLite)](#metadata-store-sqlite)
- [Body storage tiers](#body-storage-tiers)
- [Compression](#compression)
- [Retention and eviction](#retention-and-eviction)
- [Sizing guide](#sizing-guide)
- [Backend: local](#backend-local)
- [Backend: AWS S3](#backend-aws-s3)
- [Backend: Hetzner Object Storage](#backend-hetzner-object-storage)
- [Backend: MinIO](#backend-minio)
- [Backend: Cloudflare R2](#backend-cloudflare-r2)
- [IAM and bucket policy](#iam-and-bucket-policy)
- [Server-side encryption](#server-side-encryption)
- [Operations](#operations)
- [Troubleshooting](#troubleshooting)

## Overview

```
                            +---------------------------+
  request ---> middleware --+                           |
                            |   LoggingMiddleware       |
                            |   captures up to          |
                            |   MAX_CAPTURE_SIZE bytes  |
                            +---------+------------+----+
                                      |            |
                           metadata --+            +-- body + headers
                           (SQLite)                    (JSONL file)
                                                           |
                                                           v
                                               +-----------------------+
                                               |  hourly rotator       |
                                               |  current/ --> recent/ |
                                               |  recent/  --> archive/|
                                               |  archive/ --> delete  |
                                               |  + size cap evictor   |
                                               +-----------------------+
```

Every proxied request produces one row in `request_logs` and (if bodies are enabled) one JSON line in the active hourly file. The hourly file is **always** on local disk while it is being written; once the hour rolls over, the rotator promotes the closed file to the configured backend.

## Metadata store (SQLite)

| Property | Value |
|---|---|
| Backend | SQLite (`modernc.org/sqlite`, pure Go, no CGO) |
| Path | `SP_PROXY_SQLITE_PATH` (default `/data/sp-proxy.db`) |
| Journal mode | WAL |
| Headers | **Not stored in SQLite** (see [body storage](#body-storage-tiers)) |

Each `request_logs` row is ~200 bytes (ID, timestamp, merchant key, region, method, path, status, cache status, latencies, amazon_request_id, an optional body reference tuple). Headers used to live here too; at 600k rows/4h they dominated the DB at 520 MB out of 900 MB. They now live next to the payload in the JSONL file, and are fetched on demand when the dashboard opens a request detail view.

### Maintenance

After each purge pass the store runs `PRAGMA wal_checkpoint(TRUNCATE)` followed by `PRAGMA incremental_vacuum`. Without this step, pages freed by `DELETE` stay allocated inside the DB file and the file never shrinks even though the row count drops. See `internal/storage/sqlite.go:Maintain`.

Maintenance is wired into the metadata purge job (`internal/purge/purge.go`), so it runs hourly alongside the retention pass. There is no separate knob.

### Retention

`SP_PROXY_PURGE_METADATA_RETENTION` (default `720h` / 30 days) controls how long rows live. Older rows are deleted hourly. `SP_PROXY_PURGE_AUDIT_RETENTION` (default `8760h` / 1 year) covers the separate `audit_events` table.

## Body storage tiers

Bodies are stored as append-only JSONL, one entry per request, containing the request/response headers and (if captured) the request/response payload bytes. Entries are referenced from the `request_logs` row by `(body_file, body_offset, body_length)`.

Files live in three tiers, addressed by key prefix on the backend:

| Tier | Location | Writable | Compressed | Purpose |
|---|---|---|---|---|
| `current/` | **Local disk** (`SP_PROXY_BODIES_PATH/current/`) | Yes (append-only) | No | The active hour. Always local so writes are fast and crash-safe regardless of backend. |
| `recent/` | Configured backend, key prefix `recent/` | No | No | Recently-closed hourly files. Uncompressed so dashboard reads are one `GetRange` call. |
| `archive/` | Configured backend, key prefix `archive/` | No | Yes (zstd by default) | Long-term storage. Compressed to reclaim the ~4-10x redundancy across repeated headers. |

When the hour rolls over, the rotator:

1. Closes the writer on `current/2026-03-25-14.jsonl`.
2. Uploads it to `recent/2026-03-25-14.jsonl` on the backend.
3. Deletes the local copy.

When a file in `recent/` is older than `SP_PROXY_BODIES_RECENT_MAX_AGE` (default 24h) the rotator:

1. Streams `recent/<file>` through the configured codec.
2. Uploads the compressed stream to `archive/<file>.zst` (or `.gz` / no suffix for `none`).
3. Deletes the `recent/` object.

Files in `archive/` older than `SP_PROXY_BODIES_ARCHIVE_MAX_AGE` (default 720h / 30d) are deleted outright.

### Why three tiers?

- `current/` stays local because every proxied request appends to it. Pushing to S3 per-request would add a network hop to the hot path and risk partial writes on crash.
- `recent/` stays uncompressed so the dashboard can seek with a byte-ranged GET when a user opens a request detail. Compression would force a full-file decompress on every dashboard click.
- `archive/` compresses because after ~24h nobody reads the data routinely; the cost of an on-demand decompress beats paying for terabytes of hot storage.

## Compression

`SP_PROXY_BODIES_COMPRESSION`: `zstd` (default) | `gzip` | `none`.

| Codec | Typical ratio | Notes |
|---|---|---|
| `zstd` | 8-12x for SP-API traffic | Recommended. Fast encode/decode; the repeated headers (`User-Agent`, `x-amz-*`, `Content-Type: application/json`) compress extremely well. |
| `gzip` | 6-8x | Use if the downstream tool that reads archived files speaks only gzip (rare). |
| `none` | 1x | For debugging only. Files in `archive/` stay uncompressed; retention still applies. |

The codec only affects the `archive/` tier. `recent/` is always uncompressed.

## Retention and eviction

There are **two** eviction paths and they work independently:

### Age-based (per tier)

Run every hour by the rotator:

- `recent/` to `archive/` when `file.ModTime < now - RECENT_MAX_AGE`
- `archive/` deleted when `file.ModTime < now - ARCHIVE_MAX_AGE`

### Size-based (across all tiers)

Safety net for when traffic spikes faster than age-based eviction can keep up. Run every hour by the rotator after the age-based passes:

- Sum bytes of every object across `current/` + `recent/` + `archive/`.
- If the total exceeds `SP_PROXY_BODIES_MAX_BYTES` (default 8 GiB), delete oldest-first (by `ModTime`) until the total fits, **skipping the active hour** so the writer never has the rug pulled.
- For every file deleted, null out `(body_file, body_offset, body_length)` on matching `request_logs` rows via `Store.NullifyBodyRefs`. Dashboard detail views then see "body no longer available" instead of a dangling pointer.

Set `SP_PROXY_BODIES_MAX_BYTES=0` to disable the size evictor; retention is then purely age-based.

## Sizing guide

Traffic shape assumed: ~150k req/h, JSON payloads, zstd compression, default 256 KiB capture cap.

| Retention | Expected footprint |
|---|---|
| `RECENT=24h`, `ARCHIVE=720h` (defaults) | ~15-25 GiB archive, ~2-4 GiB recent |
| `RECENT=72h`, `ARCHIVE=720h` | ~15-25 GiB archive, ~6-12 GiB recent |
| `RECENT=24h`, `ARCHIVE=168h` (7d) | ~4-7 GiB archive, ~2-4 GiB recent |

Rule of thumb: **size the volume for `current/` + `recent/` + a safety margin for the rotator's staging buffer**. Everything older than `RECENT_MAX_AGE` can live on S3/Hetzner and never touch local disk.

If you run `BODIES_BACKEND=local`, `MAX_BYTES` is your wall. Pick it to be 60-70% of the volume size so the size evictor always wins against the filesystem filling up.

## Backend: local

```bash
SP_PROXY_BODIES_ENABLED=true
SP_PROXY_BODIES_BACKEND=local
SP_PROXY_BODIES_PATH=/data/bodies
SP_PROXY_BODIES_MAX_BYTES=8589934592   # 8 GiB
```

`/data/bodies/` will contain `current/`, `recent/`, and `archive/` subdirectories. Nothing else is needed. Use this for single-node deployments, development, and CI.

## Backend: AWS S3

```bash
SP_PROXY_BODIES_ENABLED=true
SP_PROXY_BODIES_BACKEND=s3
SP_PROXY_BODIES_PATH=/data/bodies       # still used for current/ (active hour)

SP_PROXY_S3_BUCKET=my-proxy-bodies
SP_PROXY_S3_REGION=eu-central-1
# Leave these blank to use the default AWS credential chain
# (IAM instance role on EC2/EKS, environment, ~/.aws/credentials, IRSA, etc.)
SP_PROXY_S3_ACCESS_KEY=
SP_PROXY_S3_SECRET_KEY=
# SP_PROXY_S3_ENDPOINT is blank for real AWS
# SP_PROXY_S3_PATH_STYLE is false for real AWS
```

Real AWS uses virtual-hosted-style URLs (`<bucket>.s3.<region>.amazonaws.com`); leave `PATH_STYLE=false`.

Preferred credential setup in production:

- **EC2 / EKS pod**: attach an IAM role, leave `ACCESS_KEY`/`SECRET_KEY` blank. The AWS SDK picks up the instance profile or IRSA token automatically.
- **ECS task**: same pattern, leave them blank, use task role.
- **Local dev or outside AWS**: set explicit `ACCESS_KEY` and `SECRET_KEY` for an IAM user scoped to the bucket.

See [IAM and bucket policy](#iam-and-bucket-policy) for the minimal permissions.

## Backend: Hetzner Object Storage

Hetzner's Object Storage is S3-compatible and lives at `https://<endpoint>.your-objectstorage.com` where `<endpoint>` is one of `fsn1`, `nbg1`, or `hel1` (Falkenstein, Nuremberg, Helsinki).

```bash
SP_PROXY_BODIES_ENABLED=true
SP_PROXY_BODIES_BACKEND=s3
SP_PROXY_BODIES_PATH=/data/bodies

SP_PROXY_S3_BUCKET=my-proxy-bodies
SP_PROXY_S3_REGION=fsn1                                      # matches endpoint region
SP_PROXY_S3_ENDPOINT=https://fsn1.your-objectstorage.com     # region host, not bucket host
SP_PROXY_S3_ACCESS_KEY=HCRR_ACCESS_KEY_FROM_CONSOLE
SP_PROXY_S3_SECRET_KEY=HCRR_SECRET_KEY_FROM_CONSOLE
SP_PROXY_S3_PATH_STYLE=false                                 # Hetzner supports virtual-hosted style
```

Generate credentials under **Hetzner Cloud Console > Project > Security > Object Storage > "Generate credentials"**. They are project-scoped, not bucket-scoped, so create a dedicated project per proxy if you need isolation.

Create the bucket in the console before starting the proxy; Smart Proxy does not create buckets.

**Why region matches endpoint host**: Hetzner expects the region label in the SigV4 signature to match the endpoint's datacenter code (`fsn1`, `nbg1`, `hel1`). A mismatch manifests as HTTP 403 with `SignatureDoesNotMatch`.

## Backend: MinIO

MinIO is S3-compatible but requires **path-style addressing** by default (bucket name is a path segment, not a subdomain):

```bash
SP_PROXY_BODIES_ENABLED=true
SP_PROXY_BODIES_BACKEND=s3
SP_PROXY_BODIES_PATH=/data/bodies

SP_PROXY_S3_BUCKET=my-proxy-bodies
SP_PROXY_S3_REGION=us-east-1             # arbitrary but required non-empty value
SP_PROXY_S3_ENDPOINT=https://minio.internal:9000
SP_PROXY_S3_ACCESS_KEY=minioadmin
SP_PROXY_S3_SECRET_KEY=minioadmin-secret
SP_PROXY_S3_PATH_STYLE=true              # REQUIRED
```

Create the bucket before starting the proxy, e.g. `mc mb local/my-proxy-bodies`.

> **Use `https://`, not `http://`.**
> The proxy does not enforce the scheme on the configured endpoint, but a plain-`http` endpoint sends body uploads (PII-redacted but otherwise plaintext) and access keys (signed with SigV4 over the wire) without TLS. Run MinIO behind TLS, either with [MinIO's built-in TLS support](https://min.io/docs/minio/linux/operations/network-encryption.html) or behind a TLS-terminating reverse proxy. Smart Proxy logs a runtime warning at startup when a non-`https` S3 endpoint is configured and refuses to start in production mode (`SP_PROXY_ENV=production`).

## Backend: Cloudflare R2

R2 uses an account-scoped endpoint and supports virtual-hosted-style addressing:

```bash
SP_PROXY_BODIES_ENABLED=true
SP_PROXY_BODIES_BACKEND=s3
SP_PROXY_BODIES_PATH=/data/bodies

SP_PROXY_S3_BUCKET=my-proxy-bodies
SP_PROXY_S3_REGION=auto                                                  # R2 accepts "auto"
SP_PROXY_S3_ENDPOINT=https://<account_id>.r2.cloudflarestorage.com
SP_PROXY_S3_ACCESS_KEY=<r2_access_key>
SP_PROXY_S3_SECRET_KEY=<r2_secret_key>
SP_PROXY_S3_PATH_STYLE=false
```

Generate the `<account_id>`, access key, and secret in the Cloudflare dashboard under **R2 > Manage R2 API Tokens**.

## IAM and bucket policy

The proxy needs to Put, Get (including ranged), List, Stat, and Delete objects within the bucket. It does **not** need to create or destroy buckets, read ACLs, or touch anything outside its bucket.

### Minimal IAM policy (AWS)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ProxyBodyBucketObjects",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:AbortMultipartUpload"
      ],
      "Resource": "arn:aws:s3:::my-proxy-bodies/*"
    },
    {
      "Sid": "ProxyBodyBucketList",
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": "arn:aws:s3:::my-proxy-bodies"
    }
  ]
}
```

`s3:ListBucket` on the bucket itself (not the object ARN) is required so the rotator can enumerate `recent/` and `archive/` prefixes via `ListObjectsV2`. `DeleteObject` covers both the age-based and size-based evictors. `AbortMultipartUpload` only matters if you ever use the multipart path (the proxy does not today but it's harmless to include).

For Hetzner / MinIO / R2, translate to the provider's own policy format; the action verbs are the same.

### Lifecycle rules (optional)

Smart Proxy already manages retention via its own rotator. If you also set a bucket-level lifecycle rule (e.g. "delete everything under `archive/` after 45 days"), make sure it is **looser** than `SP_PROXY_BODIES_ARCHIVE_MAX_AGE`, otherwise the provider will delete files the dashboard still thinks are available and the orphan cleanup won't run. Preferred: let the rotator own the deletion timeline.

## Server-side encryption

Bodies stored on S3 contain PII-redacted (but otherwise plaintext) request/response payloads. Protect them at rest by enforcing server-side encryption on every PutObject. Smart Proxy can set the SSE header itself, and the bucket policy can reject any unencrypted upload as a defense-in-depth check.

### Enabling SSE in the proxy

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_S3_SSE` | _empty_ | One of `AES256`, `aws:kms`, `aws:kms:dsse`. Empty = rely on bucket-default encryption. |
| `SP_PROXY_S3_SSE_KMS_KEY` | _empty_ | KMS key ARN or alias. Only honored for `aws:kms` and `aws:kms:dsse`. |

Examples:

```bash
# AES256 (SSE-S3): cheapest, no KMS dependency, AWS-managed keys.
SP_PROXY_S3_SSE=AES256

# SSE-KMS with a customer-managed key. Logs every encrypt/decrypt to CloudTrail
# and lets you revoke decryption by disabling the key.
SP_PROXY_S3_SSE=aws:kms
SP_PROXY_S3_SSE_KMS_KEY=arn:aws:kms:eu-central-1:123456789012:key/abcd-...

# Dual-layer SSE-KMS for regulated workloads.
SP_PROXY_S3_SSE=aws:kms:dsse
SP_PROXY_S3_SSE_KMS_KEY=alias/proxy-bodies
```

`AES256` works on any S3-compatible store (AWS, MinIO, R2, Hetzner). The `aws:kms*` variants are AWS-only.

When `SP_PROXY_S3_SSE` is empty the proxy does not set the header; the object inherits the bucket's default encryption setting. New AWS S3 buckets enable SSE-S3 (`AES256`) by default since 2023. Hetzner, R2, and MinIO encrypt at rest with provider-managed keys. Setting `SP_PROXY_S3_SSE` explicitly is still recommended so the encryption mode does not change silently if a default later moves.

### Bucket policy: deny unencrypted uploads

Use this AWS bucket policy to reject any PutObject that does not declare encryption. The two `Deny` statements mirror each other: the first blocks puts with no encryption header at all, the second blocks puts with the wrong encryption mode. Pick one or both depending on whether you allow `AES256` only or require KMS.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DenyUnencryptedPut",
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::my-proxy-bodies/*",
      "Condition": {
        "Null": {
          "s3:x-amz-server-side-encryption": "true"
        }
      }
    },
    {
      "Sid": "DenyWrongEncryption",
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::my-proxy-bodies/*",
      "Condition": {
        "StringNotEquals": {
          "s3:x-amz-server-side-encryption": "AES256"
        }
      }
    }
  ]
}
```

Replace `AES256` with `aws:kms` if you require KMS. Test the policy with the proxy running before flipping production: a denied PutObject manifests as `AccessDenied` in the rotator logs, and `recent/` upload will fail.

For Hetzner / MinIO / R2 the bucket policy syntax differs but the concept is the same: gate PutObject on the presence of the SSE header. R2 and Hetzner currently encrypt-at-rest with provider-managed keys regardless; the SSE header is mostly a forward-compatibility hedge.

## Operations

### Observability

- Scheduler and rotator events surface as `slog` lines (`bodies: promoted`, `bodies: archived`, `bodies: evicted`, `metadata purged`).
- The dashboard's log detail view reads bodies on demand. A row with `hasBody: false` means the body has been evicted (age or size); the rest of the metadata is still queryable.
- `/metrics` (Prometheus) does not yet expose body-tier sizes. Use the provider's native metrics (CloudWatch, Hetzner Dashboard, R2 metrics) to watch bucket size.

### Backups

The `current/` directory lives on the local volume. If you snapshot the volume you capture the active hour. Everything else is on the backend, so the backend's built-in durability is the backup for `recent/` and `archive/`. No separate backup of SQLite is needed for body-reference integrity since the orphan-null logic handles missing files gracefully.

### Draining before decommission

Before deleting the local volume on backend=s3:

1. Stop the proxy (prevents new writes to `current/`).
2. Wait until the rotator's next tick promotes the last `current/` hour to `recent/`, or manually move it.
3. Delete the volume.

## Troubleshooting

### DB file isn't shrinking after purge

The rotator has to run at least one maintenance pass for SQLite to release pages. `PRAGMA auto_vacuum=INCREMENTAL` + `PRAGMA incremental_vacuum` is what does the actual filesystem-level reclaim. Check that the `metadata-purge` job is logging hourly; the Maintain call happens right after the delete in `internal/purge/purge.go`.

### `SignatureDoesNotMatch` on S3 requests

Region label in `SP_PROXY_S3_REGION` does not match what the endpoint expects. AWS wants the bucket's actual region (`eu-central-1`); Hetzner wants the datacenter code (`fsn1`); MinIO accepts anything non-empty; R2 accepts `auto`. Fix the env var and retry.

### `InvalidBucketName` or 404 on PUT

Path-style vs. virtual-hosted mismatch. MinIO needs `SP_PROXY_S3_PATH_STYLE=true`; AWS / Hetzner / R2 work without it. If you use a custom endpoint and the provider docs mention "path-style", set it true.

### Local disk fills up with backend=s3

`current/` grows faster than the hour rolls over. Likely causes:
- `SP_PROXY_BODIES_MAX_CAPTURE_SIZE` is too high for your traffic volume. Default is 256 KiB; drop to 128 KiB or lower if payloads are consistently large.
- A request storm of very large (>1 MiB) responses. The cap silently truncates each response to the configured size, but captured bytes still accumulate for the full hour before being moved off-node.
- Traffic is so heavy that one hour of `current/` exceeds the available local disk. Shorten the rotation window is not supported today; in this case, raise the volume size or reduce capture size.

### `no body stored for this request` on dashboard

Expected for any of:
- Cache HITs: the proxy stored only a reference to the original request's body, and either followed the reference but didn't store a local body (correct behavior), or the original has been evicted.
- Rows whose body file was evicted by age or size. The `body_file` pointer was nulled out; the metadata row stayed.
- `SP_PROXY_BODIES_ENABLED=false`: no bodies ever captured.
