# Security Policy

Please report security issues via `security@zerodenet.org`.

- Do not commit secrets (token, database credentials, private keys).
- Use least privilege for SSH operations.
- Any critical vulnerability should be disclosed in private for at least 14 days before public.

## Security checklist

- CI should fail on static analysis issues in authentication and payment callback modules.
- SSH credentials in node management should be encrypted at rest.
- SSH connections must pin a verified SHA256 host-key fingerprint; insecure host-key callbacks are forbidden.
- Back up `ZBOARD_CREDENTIAL_ENCRYPTION_KEY` separately and never commit it.
- Traffic reports must use an independently rotated node credential, HMAC-SHA256 over the exact request body, a bounded timestamp, a one-time nonce, and an idempotent report ID.
- Node report secrets are shown only when created or rotated; do not log or persist them in the browser.
- Limit all admin operations by role + audit logs.
