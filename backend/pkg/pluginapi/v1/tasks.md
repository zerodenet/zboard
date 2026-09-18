# Registered plugin tasks

Native plugins can declare `zboard.task.v1` and implement the optional `PluginControl.RunTask` RPC. Task declarations are part of the signed manifest and are registered automatically after import. Disabled plugins remain visible in **Operations → Background tasks**. Execution requires an enabled, active and admitted native plugin on the host holding the plugin lease. There is no arbitrary host-command or dynamic task-registration API.

Add these fields to the existing native plugin manifest (retain its identity, requirements, executable and file hashes):

```json
{
  "capabilities": ["zboard.config.v1", "zboard.task.v1"],
  "contributions": {
    "pages": [],
    "tasks": [
      {
        "id": "refresh-catalog",
        "title": "更新目录",
        "interval_seconds": 300,
        "timeout_seconds": 30
      }
    ]
  }
}
```

`GetInfo` must report the same capabilities as the signed manifest. Use the generated SDK types `TaskRunRequest` and `TaskRunResult` in `RunTask`. Dispatch only known `task_id` values; return `succeeded=true` only after the work completes. The request also carries `run_id`, the installed generation and configuration revision. Configuration remains owned by the plugin's existing `ApplyConfig` implementation. Tasks gain no additional data access; declare `zboard.storage.v1` separately if private storage is needed.

- At most 32 tasks per plugin, with unique IDs matching `[a-z0-9][a-z0-9-]{0,63}`.
- Interval: 10–604800 seconds; timeout: 1–300 seconds. A title is required and limited to 160 UTF-8 bytes.
- Initial execution waits one interval after activation/discovery. Subsequent executions wait one interval after completion; no overlapping or accumulated ticks. Shared scheduling checks run every second; plugin lifecycle registration is reconciled by the supervisor.
- Plugin tasks and core jobs share one database admission budget (initial capacity four), with one task per plugin resource. Due tasks awaiting a slot are shown as queued.
- Maintenance pauses dispatch and cancels active RPCs. Disable, replacement, shutdown or lease loss prevents old executions from being confirmed as successful. Plugins must honor the RPC context and use their own business idempotency keys for external side effects.
- A reported failure is recorded and becomes eligible in the next period. Timeout, lost RPC response, or stale-generation completion becomes an uncertain result and blocks that plugin resource until an administrator records an audited verification outcome. There is no immediate retry storm. RPC errors are shown as generic failures to avoid exposing plugin output or secrets.
- Registration is persisted in the installed manifest. Plans, execution intents and attempts are stored by the shared host task service. Restart retains pending intents and history. Periodic plans coalesce missed periods and never promise exactly-once external side effects. Generation/configuration changes invalidate old handlers; old outcomes remain in history.
- Existing plugins and SDK servers embedding `UnimplementedPluginControlServer` remain compatible. An old host rejects the unknown task capability; a plugin that declares tasks but does not implement `RunTask` reports execution failures.

The host cannot discover private goroutines/timers inside a plugin. Move such work into `RunTask` and remove its old timer to avoid executing it twice. The host API `/api/v1/admin/runtime-jobs` exposes `plugin_tasks` (null when the host is unavailable) and `plugin_task_concurrency`; plugin task data includes ownership, interval, timeout, state, result, counters and next planned time.

## Requesting an additional execution

Native task plugins can use `HostTasksFromEnvironment()` and close the returned client when finished. `Submit(ctx, taskID, key)` enqueues an already declared task in the same durable host queue; it does not execute the task synchronously. `List(ctx, limit, offset)` returns only that plugin's execution history (default 20, maximum 100). The host supplies ownership, handler, timeout, execution group and resource lock. Neither arbitrary payloads nor another plugin's identity are accepted.

Use a stable business key of at most 128 bytes, and reuse it when a submission response is lost. Deduplication is scoped to the installed task and its version/generation/configuration revision; changing that revision starts a new key scope. This does not guarantee exactly-once external side effects. A task must not wait for another task under the same plugin resource to complete: that resource is held until the current task returns.

The SDK uses a private process-authenticated Unix socket. Startup/reconciliation or lifecycle lock contention can return HTTP 503; undeclared tasks and unauthorized processes are rejected. Requests are limited to 4096 bytes. The host limits queued work, including delayed runs: 4096 deployment-wide, 1024 for all plugins, and 128 per plugin. One maintenance request has a reserved admission slot. HTTP 429 with Retry-After: 5 indicates backpressure; retry with the same business key. Previously accepted matching keys still return their existing record when the queue is full. Scheduled tasks retain their due time and retry admission on a later scheduler tick. Request-rate limiting is separate from these queue-depth limits.
