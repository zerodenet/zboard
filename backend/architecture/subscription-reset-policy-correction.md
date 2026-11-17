# Administrative reset policy correction

## Independent operations

An administrator may correct an existing subscription's future reset policy via
the existing subscription quota command. `reset_policy` is optional: omitting it
preserves the original quota-edit behavior; supplying it selects a prospective
schedule correction, including recalculation when the policy itself is unchanged.
The original subscription start is the monthly/yearly purchase-day anchor. The
next occurrence is strictly after the correction time (and after a future start).
Policies 0 and 5 clear the next date. Periodic reset is unavailable for perpetual
and fixed/exhaustion-ended entitlements.

A schedule correction does not execute an overdue old schedule, grant missed
cycles, change current cumulative usage/baseline/quota, revive credentials or
enqueue a configuration publication. Explicit quota edits may accompany the
command and retain their existing behavior. Administrator recheck, subscription
row lock, idempotency and before/after audit remain in the owning transaction.
No order or payment snapshot is rewritten to make a correction pass.

The admin editor exposes reset policy, reset quota and optional date recalculation
alongside current-cycle quota fields. Only edited values are sent; the server
returns the authoritative updated detail on reload. Changed policy shows a short
prospective-effect notice. Dates are calculated on the server, not approximated
using a separate frontend calendar implementation.

## Optional plan synchronization

`sync_reset_policy` is independent of `sync_traffic_quota`. Both default to false
in the editor and are preserved in same-account reauthentication drafts. Reset
synchronization requires an explicit reset policy and selects still-unended,
unelapsed subscriptions, excluding perpetual and fixed-quota instances. It
recalculates the next occurrence even if an old subscription has the right policy
but a missing/stale next date. It never performs catch-up grants.

The existing plan-before-subscription lock order and bounded 128-row paging are
reused. The complete group/quota/reset authority union is locked in ascending ID
order before publication effects. All pages, the plan revision and one zero-delta
`reset_policy_sync` quota event per changed subscription commit or roll back
together. Unchanged schedules generate no new event. No new table or periodic
per-subscription records are introduced.

An unchanged ordinary cycle whose reset quota already equals the requested plan
quota skips resize evidence reconstruction. Mixed/add-on source changes continue
through existing paid-source validation. A reset-only sync does not require paid
source reconstruction; a real quota resize does. This operation does not reprice
historical payment/refund facts or manufacture missing financial evidence.

## Regression evidence

Tests exercise leap-month anchor preservation, future starts, disabled policies,
no implicit catch-up, unchanged paid order and current-cycle usage, idempotent
retries, fixed-quota rejection, 131 subscriptions crossing a page boundary,
terminal/perpetual/fixed exclusions and complete rollback on a second-page event
write failure. Existing quota-sync and timed paid-value regressions remain in the
validation set. Frontend tests cover partial payloads, missing-date recalculation,
independent sync choices and their restoration after reauthentication.

Production subscription #2 was inspected read-only: annual Pass Alu, policy 0,
no next reset date, 300 GiB quota. Its schedule was not changed by this work.
This is a local implementation; no build, tag, push, deployment or production
batch correction was performed.
