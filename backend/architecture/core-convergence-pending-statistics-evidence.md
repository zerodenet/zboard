# P4-03: explicit pending traffic statistics

The fixed 4dfb664 hourly comparison failed: 112 of157 trend requests returned
503. The complete failure and original gates remain in
[the comparison report](core-convergence-capacity-hourly-comparison.md). Final
accounting success did not make that run pass.

Normal asynchronous projection lag now returns HTTP200 with
`projection_pending:true`, the authorized scope and validated calendar range.
`points`, `record_count`, `connection_sample_count`, `peak_connections`,
`truncated`, `subscriptions` and `as_of` are null. This is an unknown read state,
not zero traffic or a complete graph. `Cache-Control:no-store` prevents pending
snapshot caching; `X-Zboard-Statistics-State` distinguishes pending/complete.
Only the typed pending condition follows this path. Storage faults, cancellation,
invalid filters and revoked permissions remain failures. Existing complete
response values and snapshot cache semantics are preserved.

Both calendar/principal decorators preserve the pending wire response without
unmarshalling null into numeric zeros or querying principal statistics. No raw
history fallback, request-driven queue draining, settlement or quota writer was
added. Business quota and durable projection intents are unchanged. The OpenAPI
now documents both previously undocumented trend routes and their discriminated
complete/pending contracts; other statistics routes retain their existing errors.

The frontend validates scope and from/to, retains only a same-scope complete
snapshot and its original as_of while updating, and shows unknown on cold pending.
Identity, permission, date, filter and timezone changes cannot borrow another
snapshot. Real request failures still show errors. The date-range regression was
first reproduced by two failing tests, then fixed.

## Recovery validation

Cloud workspace, Go1.26.8, Node22.23.0 and pnpm11.9.0. New logs persist in ignored
`.codex-local-artifacts/recovery-validation/`; the restart removed old /tmp logs.

- `GOMAXPROCS=2 go test -p 2 ./internal/handler -run '^TestTrafficTrendPending' -count=1 -timeout 3m`: pass1.131s. Four route/decorator variants, user/admin authorization, null wire values, cache invalidation, timezone/scopes, queue-not-drained, quota independence and real infrastructure failures.
- Frontend narrow suite: seven files,104/104 tests passed. The previous incorrect range was preserved in a failing-test log.
- Complete frontend command from current CI with its original six exclusions:198 files passed, two existing files skipped;1010 tests passed, eight existing tests skipped,147.79s. No exclusion/skip added.
- `pnpm --dir frontend build`: vue-tsc and Vite passed,14.11s Vite.
- The full backend, real MySQL and fixed-source capacity comparison are recorded separately when executed; no older CI result is substituted for this source.

This batch has no migration or change to financial/entitlement/received-event
transactions. Rollback restores the previous read contract/UI and its known503
availability limitation; no facts or queue state are deleted. A successful HTTP
capacity gate must be reported separately from the pending/complete observation
ratio. No claim of P4/P6/P7 completion, absolute zero overuse, archival safety,
independent worker benefit or production acceptance follows from this change.
