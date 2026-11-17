# Isolated serial browser acceptance

This entry builds and runs the existing session, closed-order consumption-refund,
and recurring-consent browser cases at one exact clean commit. It uses only fresh
SQLite fixtures and loopback HTTP. It does not contact a real identity provider,
Core node, payment gateway or coupon service. Financial amounts, permissions,
explicit replay checks and session no-write-replay assertions remain in the
existing cases.

Prerequisites: the repository Go/Node/pnpm versions, Python3.12 or newer, the
pinned Python client from `browser-acceptance-requirements.txt`, and an already
installed Chrome/Chromium. Install frontend dependencies with the repository's
frozen lockfile before running. The entry installs nothing itself. It requires
a non-root Linux user and enables Chromium's sandbox explicitly. If the existing
browser or sandbox cannot run, acceptance fails; do not add no-sandbox,
certificate-ignore, permission changes or alternate dependency sources to pass.

Use a new private directory and a new summary filename outside the checkout:

```sh
python3 scripts/acceptance-browser-serial.py \
  --checkout "$PWD" --revision "$(git rev-parse HEAD)" \
  --chromium "$(command -v google-chrome)" \
  --private-dir /ABS/NEW-PRIVATE-DIRECTORY \
  --summary /ABS/NEW-SAFE-SUMMARY.json
```

The entry serially compiles the real server, fixture seed tool and handler test
binary, then builds the frontend once. It records source before/after hashes,
binary hashes and source/dist manifests, serves immutable copies of that dist,
and verifies the binaries/dist remain unchanged. It preserves the existing
five-minute refund test and four-minute recurring test timeouts and their internal
phase deadlines. Each case runs once; the first failure stops dependent cases,
which remain unexecuted. All raw command output stays private. Only explicitly
owned child process groups may be stopped at their deadlines; no existing server,
database, browser or host process is selected or cleaned.

The session case launches the real application server. Refund and recurring
cases run the existing real-handler loopback test servers from their required
handler working directory; they do not prove the complete production router or
middleware chain. Refund uses an authorized fixture PaymentFact and explicit
future scheduler clock, not a live gateway or natural-term passage. Recurring
builds and signs the repository's native quote fixture using temporary keys;
it is not an external provider conformance result. Node delivery, real OAuth,
MySQL browser races, capacity and production operation remain outside scope.

`isolated-browser-acceptance` is a one-shot GitHub workflow: explicitly dispatch
it or add `validation:browser-full` to the PR. Before the workflow is present on
the default branch, use the PR label. Retaining the label does not rerun
it on every push. It uses the runner's existing browser and installs only the
pinned Python client in a temporary virtual environment through the default
package source once. It never installs a browser, runs apt, creates a system
browser symlink or changes browser/host security policy. Missing dependencies or
network refusal fail visibly without retry or source substitution. No user-owned
machine or separately maintained environment is required.

The workflow uploads only the safe summary: fixed case IDs, booleans, counts,
hashes and fixed error classes. The private directory contains generated test
credentials, SQLite data, browser state/screenshots, HTML, raw logs and financial
fact evidence. Do not commit, publish or upload that directory. GitHub runner
teardown removes its ephemeral files; this entry itself never deletes existing
fixtures or evidence. A partial run, old-head result, skipped case or build-only
success cannot become a complete browser pass.
