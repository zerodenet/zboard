# Public native action errors (protocol/bridge v1)

`HandlePageAction` serves authenticated business pages and slots. Existing
successful `PageActionResponse.result_json` remains arbitrary valid JSON, at
most 64 KiB. ZBoard does not infer failure from legacy JSON keys such as `error`,
`ok` or `status`; existing plugins keep their original result semantics.

## Explicit business rejection

Return the public SDK error when the plugin has decided that an action failed:

```go
return nil, pluginv1.NewActionError(
    "resource_busy", "The resource is busy. Please try later.", true,
)
```

This produces standard gRPC `FailedPrecondition` with exactly one `ErrorInfo`:

- `domain`: `zboard.plugin.action.v1`
- `reason`: the plugin-owned code, matching `[a-z][a-z0-9_]{0,63}`
- `metadata.public_message`: explicit public UTF-8 text, 1–1024 bytes, no
  leading/trailing whitespace or control characters except newline/tab
- `metadata.retryable`: exactly `true` or `false`

The complete RPC status is limited to 2048 encoded bytes. Unknown details,
additional metadata, invalid fields and oversized values fall back to a generic
host runtime error. Raw RPC messages, stack traces, credentials, configuration,
provider bodies and `HostCallError` text are never implicitly public. Plugins
must choose a safe explanation; the host cannot detect a secret deliberately
placed in a valid public explanation. HTML-looking text is literal text.

`HostCallError` is the opposite direction (plugin calling host capabilities).
It is not this contract. Do not convert permission or infrastructure failures
to a business rejection merely to expose their internal error strings.

## HTTP and browser bridge

Both `page.call` and host-owned `checkout.call` return HTTP **422**, never 401,
403, 429 or a success response for this rejection:

```json
{
  "code": 422,
  "message": "The resource is busy. Please try later.",
  "error": {
    "version": 1,
    "code": "plugin_action_rejected",
    "source": "plugin",
    "plugin_code": "resource_busy",
    "retryable": true
  }
}
```

`error.code` is selected by ZBoard. `error.plugin_code` belongs to the plugin.
Even a plugin code named `session_expired` cannot become a host session error.
The standard timestamp may also be present. Error responses contain no success
`data`. The iframe receives additive fields without changing its existing
request/token correlation:

```json
{
  "source": "zboard-plugin-host",
  "bridge_token": "current frame proof",
  "request_id": "current request",
  "ok": false,
  "error": "The resource is busy. Please try later.",
  "code": "plugin_action_rejected",
  "source_kind": "plugin",
  "plugin_code": "resource_busy",
  "retryable": true
}
```

The envelope's existing `source` is always `zboard-plugin-host`; error ownership
uses `source_kind`. Clients reject `ok:false`, preserve the code/retry flag in
the resulting error, display `error` using `textContent` or escaped framework
interpolation, and run navigation or successful UI updates only on `ok:true`.
Never render public text with `innerHTML`/`v-html`.

`retryable` allows the user to retry; it does **not** authorize automatic replay
or declare an action idempotent. The plugin owns business recovery, action
availability, external protocols and compensations. Existing concurrency,
rate, result-size, process and permission constraints remain in force.

## Lifecycle and failure handling

ZBoard rechecks the exact session and native process after the call, before
accepting either a successful result or a public error. Revocation, plugin
generation changes and frontend frame replacement reject stale results. Host
authentication/session/permission errors retain their own rules. Plugin
business failures do not invalidate the main account login.

Host action promises reject on business errors. Their callers must release
busy state in `finally`, preserve form input, close any reserved blank window
on failure and omit success navigation. A canceled/replaced component must not
apply an old response to its new context. A public error is not evidence of a
core transaction or payment result.

## Compatibility and verification

There are no protobuf fields, required grants, or protocol-version changes.
Old plugins continue to succeed or use the generic runtime-failure path.
New errors on hosts without this support degrade to the existing generic
failure; they never become success. Publish an appropriate host version range
if the plugin requires the public presentation, rather than assuming all v1
hosts have it.

From `backend`, with Node 22 first on PATH:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/handler -run '^TestPluginActionErrorsUseRealNativeAndHTTPContract$' -count=1
ZBOARD_RUN_ACTION_UI_TESTS=1 GOMAXPROCS=2 go test -p 1 ./internal/handler -run '^TestPluginActionErrorsFullHTTPUIBridge$' -count=1 -v
```

The independent signed fixture uses real native gRPC, sessions and HTTP handlers.
The opt-in UI suite mounts the real host frame and uses actual HTTP, not mocked
action responses. It covers business rejections, malformed/internal errors,
host lifecycle precedence, namespace collisions, success compatibility and
global-login isolation. `TestPluginActionErrorBrowserFixture` additionally
supports local real-browser acceptance with the signed fixture UI.
