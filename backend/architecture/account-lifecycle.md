# Account deletion and subscription delivery

Subscription delivery has no default public URL. `subscription_url_prefix` is an
administrator-only setting containing a site path, such as `/private-feed`.
An empty value disables delivery, including the legacy route. Both account and
administrator order delivery views expose the configured path and explain when
it is absent. System/API/frontend routes, queries, escaped paths and external
URLs cannot be used as a prefix.

The runtime router resolves changes without restarting the service. During the
compatibility period, the original `/api/v1/client/subscription/<token>` route
returns 301 only for an existing non-revoked subscription identity owned by an
active account. The redirect retains the token and raw query and uses
`Cache-Control: no-store`. Invalid probes receive no prefix information.
Configured requests pass through the existing subscription authorization and
rendering handler; internal canonical routing keeps bearer tokens out of access
logs. Previously configured custom paths are not aliases after a path change.
Before upgrading an existing installation, the administrator must choose its
prefix; provision that explicit value as part of the upgrade to avoid a gap.

Accounts are physically removed from `users`. Login/API/subscription credentials,
external identity bindings, shared membership associations, and unconsumed
registration events are deleted. Remaining protocol credentials are revoked and
configuration publication is queued transactionally. Outstanding automatic-debit
consent is disabled. Manual deletion requires an administrator, previews current
blockers, and rechecks them inside the deletion transaction. Administrators and
the logged-in account cannot be deleted. Unfinished subscriptions, active shared
access, wallet funds/reservations, pending orders, unsettled payments/payouts and
pending automatic renewals must be handled first. Failure rolls back deletion and
its audit together; the dialog retains the specific reason.

Historical accounting is not account authorization. `account_subjects` contains
only a never-reused numeric ID, with no email, password, login state or deletion
flag. It allocates new IDs in the account-creation transaction and survives an
account's physical removal. Orders, subscriptions, credential ownership, audit,
tickets and money/traffic facts reference this identity. Their internal financial
foreign keys and permanent report/cursor proofs remain intact. Completion of old
reports uses the historical subject when the live user no longer exists. It
neither recreates an account nor enables its subscription.

Migration 0079 backfills IDs and moves historical foreign keys to subjects. MySQL
uses explicit `INPLACE, LOCK=NONE` DDL with bounded metadata-lock waiting, without
a COPY fallback or a required database trigger/global privilege. On a dedicated
connection, constraints proven by the existing FK and the copied owner-ID set are
retargeted with session checks temporarily disabled and restored before pooling.
Normal deletion keeps constraints enabled. SQLite rebuilds affected small
financial tables transactionally, preserving checks/indexes/triggers and checking
foreign keys before restoring the connection. Startup rejects missing identity
storage/allocator or a historical FK still targeting live users. Deleted accounts
cannot be reconstructed by a downgrade; the migration's down direction refuses.

`user_cleanup_inactive_days` defaults to zero (disabled). For X > 0, the waiting
period begins at registration or the most recent entitlement end, including
shared access. A newly expired paying account gets the full waiting period;
login activity alone does not protect an account with no entitlement. Existing
accounts are included when enabling the policy. The system job scans at most
500 account IDs and deletes at most 100 accounts per minute, carrying one durable
cursor and compact success history. Every account's policy, entitlement and
financial guards are rechecked under the same settings/owner coordination used
by domain writers. Clearing the setting to zero stops subsequent deletions.
The job never scans or deletes historical telemetry to remove an account.
