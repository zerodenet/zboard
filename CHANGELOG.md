# Changelog

## v0.1.0

_Planned first public release. No `v0.1.0` release has been cut._

- Public scope: users, nodes, plans, orders, subscriptions, trusted traffic accounting, and Docker deployment.
- Kubernetes deployment is not part of the v0.1.0 support scope.

## v0.0.1

- All implementation and hardening before the first public release continues to accumulate under this development version.
- Establish working monorepo baseline with backend/frontend/deploy/docs.
- Introduce initial backend flows that will be hardened before v0.1.0:
  - JWT token login/register
  - Node, Plan, Order, Subscription, Traffic summary, Admin dashboard APIs
  - Admin-only route protections
- Restrict registration privilege escalation by setting new users to non-admin by default.
- Validate protocol values in node creation/config APIs against supported protocol set.
- Frontend session handling hardened: failed token state now clears session immediately.
- Pin the Go, Node.js, and pnpm build baselines for reproducible validation.
- Add frontend dependency locking, type checking, backend tests, and CI gates.
- Remove the fixed `admin/admin123` bootstrap path and require explicit first-user credentials.
- Reject weak JWT/database production settings and redact database passwords from startup logs.
- Encrypt node SSH passwords with AES-GCM, migrate legacy plaintext, and require pinned SHA256 host keys.
- Authenticate traffic reports with per-node encrypted credentials, HMAC-SHA256 signatures, a five-minute time window, nonce replay protection, and idempotent report IDs.
- Serialize order settlement and subscription renewal, enforce terminal paid-order transitions, reconcile expired/exhausted subscriptions, and require paired server/client JSON when publishing node protocol configuration.
- Record sensitive user, order, protocol, and credential lifecycle changes in the audit log and expose a paginated admin-only audit view.
