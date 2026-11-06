# Drop Copy file output

A file output is an internal subscriber to upstream application messages. It starts with the gateway and captures selected messages even when no downstream FIX clients are logged on. It has no network listener, FIX identity, session sequence stream or resend store. Existing downstream FIX client configuration remains required; a file output does not use one of those client slots.

The export receives only messages supplied to this gateway's upstream session. It is live capture with bounded memory and disk use. It does not establish a separate venue Drop Copy connection or reconstruct messages the venue never supplied.

## Configuration

Add an optional top-level section to your configuration:

```yaml
outputs:
  - name: dropcopy_file
    type: file
    directory: ./dropcopy
    format: jsonl
    receive_message_types: ["8", "9"]
    queue_messages: 1000
    queue_bytes: 8388608
    max_file_bytes: 67108864
    max_files: 16
```

`type: file` and a unique safe `name` and dedicated `directory` are required. Paths are relative to the process working directory. The only supported format is JSONL, the default when `format` is omitted. Omitted or zero numeric limits select the values above; negative values fail validation. Without `outputs`, behavior is unchanged. Use a build containing this feature; older binaries reject the new key.

Omit `receive_message_types` to inherit `fanout.app_message_types`; `[]` captures nothing. An explicit list must be a distinct subset of the global fanout allowlist. The global deny list wins. Only upstream application callbacks are captured: no downstream submissions, FIX administrative messages, gateway-generated errors/notices, or outbound engine resend callbacks. Capture is independent of which network recipients are currently logged on and of their per-client filters. It occurs before network-message reconstruction, so a later recipient reconstruction/send failure does not remove the capture.

There can be up to eight outputs. Each has independent filtering, queue and writer. Limits are 1–4,096 queued messages, 4 KiB–64 MiB queued bytes, 4 KiB–1 GiB per file and 1–128 files. Combined configured queue bytes cannot exceed 64 MiB; combined `max_file_bytes * max_files` cannot exceed 8 GiB. Queue-byte accounting includes the record currently being written. Base64/JSON overhead counts toward the queue and file limits; select limits large enough for a whole encoded record.

## Records and possible duplicates

Every line is a JSON object with these fields:

| Field | Meaning |
|---|---|
| `received_at` | Gateway capture time in UTC, including for replayed messages. |
| `msg_type` | Original upstream application MsgType. |
| `upstream_seq_num` | Sequence number from that upstream session, for correlation. |
| `poss_dup_flag` | Original tag 43 value: `"Y"` or `"N"`; omitted when tag 43 was absent. |
| `orig_sending_time` | Original tag 122 value, when present. |
| `fix_base64` | Base64-encoded engine-received upstream FIX snapshot, including upstream headers, body and trailer. |

**PossDupFlag is preserved, not inferred or cleared.** A genuine inbound upstream replay is captured with its original duplicate metadata. A downstream client's own resend does not create another export record. No application-level deduplication is performed, so consumers must tolerate repeated business messages. An absent flag or `N` is not a guarantee that a business event is globally unique.

The full FIX snapshot also preserves tags 43/122, group layout, scalar extension tags, delimiters and non-UTF-8 bytes as supplied by the engine. Base64 avoids JSON's lossy handling of invalid UTF-8. To inspect FIX content, decode `fix_base64`; the human-readable metadata fields remain directly searchable without decoding. These upstream sequence numbers and replay flags describe the captured input. Network clients continue to receive independently reconstructed per-session framing.

## Files, retention and failure behavior

Use a dedicated directory, separate from both FIX store roots and other output directories, including ancestor/descendant relationships. Resolved filesystem paths are checked at startup. The directory is exclusively managed by this output: unrelated files or subdirectories block initialization/rotation, and capture symlinks are rejected. Do not modify it while the writer is running. New directories/files use restrictive permissions; existing host-directory permissions remain the operator's responsibility.

Files have names such as `capture-20261106T140500.123456789Z-123456789.jsonl`. A new file is created on the first selected message, when the next record would exceed `max_file_bytes`, or when capture time crosses a UTC date boundary. Records are never split deliberately across files. Each complete write is followed by a file sync before incrementing the written counter. Files from a previous process are retained but never reopened for append: after a crash, a previous file can have an incomplete final line, and a fresh process starts a new file.

At startup and before opening a new file, retention removes the oldest closed captures as needed to stay within `max_files` (existing files are ordered by modification time, then filename). Existing captures larger than the configured per-file limit cause startup failure. The nominal output data budget is `max_file_bytes * max_files`, separate from FIX-store retention budgets and filesystem allocation overhead. Files are not required FIX replay history and may be deleted according to this explicit export-retention policy.

Queue overflow, an oversized encoded record, or a write/flush/rotation failure latches the affected output unhealthy. That output stops accepting new captures; pending records that cannot be written and later matching arrivals are counted as dropped. A single `file_output_failed` diagnostic records a hashed output label and fixed reason without payloads, paths or raw storage errors. `/ready` becomes false; normal FIX forwarding and other outputs continue. Fix the underlying problem and restart to resume capture. A failed write/sync can leave a partial or uncertain record on disk; a dropped count does not prove that no bytes were written.

Healthy shutdown drains accepted work, syncs writes and closes files before releasing the directory lease. The CLI's existing shutdown deadline still applies if disk I/O hangs. A crash or forced exit can lose in-memory/in-flight work; file output provides no exactly-once, complete-audit or automatic offline catch-up guarantee.

The `.file-output.lock` directory prevents two writers using the same output directory. After a crash, verify that the previous process/container is gone before removing only that empty lock directory and restarting. Preserve capture files; do not delete FIX stores or reset sequence state to recover an export lease.

## Docker host files

Mount a writable host directory into the container and point the output at it:

```yaml
# In the gateway's YAML configuration:
outputs:
  - name: dropcopy_file
    type: file
    directory: /app/dropcopy
    receive_message_types: ["8", "9"]
```

For Docker Compose, add this to your gateway service:

```yaml
volumes:
  - ./dropcopy:/app/dropcopy
```

For `docker run`, add `--mount "type=bind,source=<absolute-host-directory>,target=/app/dropcopy"` to the command in the [Docker setup guide](DOCKER_SETUP.md). Create the host directory first and make it writable by the image's nonroot user (UID/GID 65532 on Linux). Keep this mount writable even if the container root filesystem is read-only. The files appear on the Docker host; remote FIX clients do not receive files on their own machines. A Docker-managed named volume is also possible when direct host-folder access is unnecessary.

The Go binary performs all file operations; the runtime image needs no shell or additional logging utility. The example `./dropcopy` directory is excluded from Git and the Docker build context. If you choose another directory, exclude it from both as well.

## Observability and access

Metrics are `fix_mux_file_output_healthy`, `fix_mux_file_output_written_total`, `fix_mux_file_output_dropped_total`, `fix_mux_file_output_queue_bytes` and `fix_mux_file_output_queue_depth`, labeled by hashed configured output name. Counters reset with the process. Written means a complete write and file sync succeeded; it is not a venue acknowledgment or a guarantee against every host/storage failure.

Capture files intentionally contain full upstream application traffic, including business identifiers and account/trade information. Keep them off public shares and protect access and backups. Metadata-only runtime logging remains unchanged; opting into file capture does not redact the exported business payload. Administrative Logon credentials are excluded because admin callbacks are never captured.
