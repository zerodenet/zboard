# Page patterns

## Admin shell

The admin shell uses a quiet navigation tree. The current domain expands its
page links directly below the domain button. The topbar holds local page search,
location and account/site links; the page header owns the title, description,
and primary actions. At 820 px and below the sidebar becomes a focus-managed
drawer. The content column uses the available width with responsive padding.

Design desktop layouts at 1280, 1440 and 1920 px before adapting them to narrow
windows. Size tables from their own available content width, not a second set of
window-width rules. Auxiliary priority-3 columns collapse at 900 px of table
space and priority-2 columns at 600 px. Keep identity, important state and common
actions available; allow table-local scrolling when they cannot fit.

Desktop pages prioritize clear, dense information and nearby actions. Mobile
pages prioritize touch interactions and fewer inputs, reusing known values and
defaults. Adapt the task flow as well as the layout; do not turn the desktop
page into a long stack of explanatory text. Use hierarchy and control state
before adding labels or prose; retain accessible names and actionable errors.

Default page headings contain the title and actions. Add a subtitle only when
it changes the user's decision; do not repeat the page title, list the controls
below it, or describe backend loading and storage. Conditional failures and
destructive consequences stay visible at the relevant interaction.

## Session recovery

Show the prefilled account, password and available login actions. Focus the
password when password login is known to be available. External-only accounts
start with the third-party action and can explicitly choose password login.
Do not put draft storage duration, recovery implementation or permission checks
in the dialog. Show the short data-loss consequence only when switching accounts.
Identity verification, permission refresh and prevention of payment replay
remain required behavior even though their implementation is not described here.

## Data page

Use `PageHeader`, `WorkbenchFilterBar`, `DataWorkbench`, `DataTable`,
`TablePager`, and a `DetailDrawer` when quick inspection is useful.
List state belongs in the URL and server-side table reader. Keep current data
visible during refresh, and show a small refresh state. Separate initial
loading, empty results, API failure, and permission failure.
Successful reads must not leave persistent banners. Keep refresh/stale/error
feedback visible; put diagnostic registries and execution catalogs in collapsed
details after the actionable content so they do not dominate the first screen.
Quiet timestamps may occupy a reserved metadata slot. Background refresh must
retain existing rows and must not insert a loading paragraph above them. Reset
the reader only when its query scope changes; failed refreshes retain the last
successful result and expose a retry next to that result.
Noncompact read-status components announce routine background refreshes to
assistive technology without inserting a visible row. Use the compact variant
when a page needs a persistent visible timestamp. First loads and failures
remain visible; errors must never turn into a successful empty state.

The node asset page is the reference data flow: it has search and filters,
selection, table status, detail drawer, edit forms, destructive confirmation,
pagination, and independent loading/error states. Keep business data and API
contracts in the page; reuse presentation and interaction through components.

## User management

Keep the email as the account identity and show an optional administrator note
in the existing secondary row. Notes are admin-only, searchable, editable in the
account editor, and absent from public/account projections. Do not duplicate
the note in a new badge or expand the row into a paragraph.

The cleanup column shows a single estimated date, or `-` when no deletion time
is determined (including protected, disabled and permanent accounts). Do not repeat
the retention policy beneath every date. A due date or eligible state is red;
protected and disabled accounts are not overdue. Never label a queued account
as deletion in progress without an actual execution state.

The account-centre cleanup notice is targeted, not a platform policy broadcast.
Show it only for enabled `scheduled` or `eligible` states. Administrators skip
this read entirely; protected accounts and users with valid subscriptions see no
notice, even when a possible future deadline exists. A failed refresh retains a
known countdown and a retry action; an unread or previously unaffected account
must not be presented as at risk without an authoritative matching state.

In the user list, show zero and unavailable balances as `-` while preserving
their different meanings in accessible labels. Keep nonzero amounts and
currency-aware wallet links intact; this is a presentation rule only.

## Settings page

Use a compact page header followed by titled sections, descriptions, fields,
and one action per section. Prefer dividers and whitespace to nested cards.
Field errors appear beside their controls.

## Subscription templates

The template name opens its editor directly. A single inline switch controls
availability; stopping a live template retains its existing confirmation and
fresh revision check. The format picker owns selection feedback. Do not repeat
the selected renderer in a second success card or explain credential injection.
Runtime controls express their state directly; client mode, DNS and TUN keep
their functional dependencies, validations and advanced editor unchanged.

## Detail page

Show the entity heading, status and metadata, tabs where needed, then content
sections. Use a sheet for quick inspection and an entire page for long
management tasks. Preserve return focus on close and Escape behavior.
The shared drawer starts at 720 px and can expand up to 1200 px for desktop work;
closing it resets its width. This is a presentation choice and does not change
the selected entity, tab, authorization or unsaved form state.

## Customer subscription access

Place the selected subscription's link and copy action before history and
monitoring. Selecting a different subscription focuses the matching access
panel. Keep management and renewal actions in the row; put secondary actions in
the shared menu and protocol telemetry in a collapsed section. Credential
generation, rotation and revocation keep their existing confirmation and
subscription-isolation rules.

## Plugin lists

The market and installed-plugin lists use the same capsule search and filters,
with selected conditions retained in the URL. Plugin names open details through
real links. Keep enable/disable, configuration and uninstall actions separate;
installation compatibility and signature checks remain in the install flow.

## Verification

Review at 1920, 1440, 1280, 1024, and 768 px. Check normal, empty, loading,
refreshing, failed, forbidden, long text, and high-volume states. Verify
Tab, Shift+Tab, Enter, Space, Escape, and arrow-key paths for every overlay.
Build and type checks are necessary but do not replace this page review.

Plugin lists use the available workspace width. Long card names remain real
links with a title only when clipped; descriptions use measured `TableText`
expansion instead of stretching every card to fit an entire document.

## Account centre

Use the real `AccountLayout` when reviewing customer pages, with its navigation,
content width and responsive table containers. A shared-component fixture alone
does not establish page-level compliance. Review overview, plans, orders and
order detail, wallet, subscription access, traffic, announcements, tickets,
security and integration credentials separately.

Use `account-section-body` for padded content and `account-form` for short forms
(maximum 480 px). Group nearby actions in `account-form-actions`; do not place
bare controls directly below a section header. Wallet summary and top-up share
a desktop row and stack on mobile. Currency selection is compact; ledger amounts
identify currency once in the header, keep exact minor-unit formatting and retain
zero transactions. Keep business and amount visible on small screens; move
auxiliary columns behind the existing column toggle and prevent sticky actions
from covering financial values. Do not change checkout intent, idempotency,
credential, password or payment-confirmation contracts for presentation.

Routine successful state needs no explanatory banner. Subscription links are
shown once with nearby quota/expiry and copy action; keep technical options
collapsed. Announcement content stays flat, readable and without decorative
rank numbers or shadows. Connection instructions for integrations are opt-in;
one-time secret handling and revocation consequences stay explicit.
