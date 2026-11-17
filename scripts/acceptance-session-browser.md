# Isolated browser session acceptance

`acceptance-session-browser.py` starts its own loopback HTTP server and fresh
SQLite fixture. It uses installed Python Playwright and Chromium; it does not
install repository dependencies or target an existing server/database.

Build the server and fixture tool from the intended backend checkout, then run:

```sh
python3 scripts/acceptance-session-browser.py \
  --server-binary /tmp/zboard-browser-cf1c8be \
  --seed-binary /tmp/zboard-acceptance-cf1c8be \
  --backend-source /workspace/zboard-verify-clone \
  --web-dir /workspace/zboard/frontend/dist
```

The checkout argument records the backend source commit; it does not rebuild or
change that checkout. Both executables must have been built from it. The runner
records executable hashes, frontend source and bundle manifests, Chromium version
and the script hash. It takes a private copy of the built frontend and rejects a
bundle that changes while being copied. Use a completed frontend build and record
any source changes between that build and the run.

Each invocation creates a private directory under
`.codex-local-artifacts/acceptance/session-browser-*`. The fixture credentials,
database, configuration, screenshots and failure diagnostics stay there. Do not
commit or publish that directory. Stdout contains only pass/fail, a stage/check
identifier and the evidence directory. The process always stops its local server.

The runner checks:

- Two concurrent protected requests receive actual HTTP 401 responses, with one
  original-page reauthentication dialog. It delays genuine server responses to
  make the concurrency repeatable; it does not fabricate API success or identity.
- Real ModalDialog focus containment, Tab handling, Escape handling and the
  browser's inert behavior. It checks the original editor, draft, URL, scroll
  position and focus after same-account recovery.
- A failed top-up POST is not replayed after recovery. The isolated database has
  no new top-up order or wallet entry, and a different account gets a fresh form.
- Revoking the same fixture account's admin role removes the old editor. The
  newly verified login still returns HTTP 200 while an administrator endpoint
  returns HTTP 403.

Expiry is injected by re-signing only the current isolated fixture token with a
past expiry, using that fixture's secret. Role changes affect only its SQLite
user row. All other page-request origins are aborted before forwarding. Blocked
resource attempts are recorded separately from requests sent.

This is SQLite/browser session acceptance. It does not verify MySQL concurrency,
successful payment fulfillment, external identity providers, production operation
or capacity. Cross-page external login draft recovery is outside its scope.
