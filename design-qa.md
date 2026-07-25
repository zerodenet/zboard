# Stripe-style administrator interface design QA

## Reference

- Stripe connected-account review dashboard:
  https://docs.stripe.com/connect/dashboard/review-actionable-accounts
- Stripe Dashboard filter catalog:
  https://docs.stripe.com/connect/dashboard/filters
- Stripe application layout and token guidance:
  https://docs.stripe.com/stripe-apps/style

## Implemented contract

- `/admin/protocols` remains the reference page, while `AdminLayout` now owns
  the shared `admin-stripe-surface` contract for all 15 administrator routes.
- Every administrator page follows the same dashboard hierarchy: restrained title and
  primary action, status overview selectors, filter chips and view utilities,
  then high-density table or settings surfaces appropriate to the resource.
- Five clickable deployment-status cards show server-backed totals for the
  current keyword, protocol and service-status scope. The selected card is the
  same `deployment` filter used by the URL and shared filter component.
- Table utilities, pagination and bulk selection remain inside one bordered
  workbench. Multi-action rows use the shared ellipsis popover instead of
  reserving width for every action.
- Table and cursor lists use the shared Stripe pagination contract: a 42px
  footer, 28px page-size selector, numeric range/page labels and 28px
  icon-only previous/next controls.
- Page refresh uses one icon-only `PageRefreshButton` with an accessible name.
  Status and metric summaries use the shared `OverviewCard`, while resource
  lists retain their existing server-backed counts and filter ownership.
- Keyword search uses the same inactive/active filter-chip contract; its input
  exists only inside the anchored popover.
- Dashed inactive `+ filter` chips.
- Anchored floating filter pickers.
- Active `field value` chips with per-filter removal.
- Shared clear-all action.
- Immediate select application and explicit text/number/date application.
- Text, number and date popovers keep local drafts and update list state only
  after Apply.
- Escape, outside-pointer close, focus return and viewport clamping.

## Verification

- Vue type checking: passed.
- Vitest: 55 files, 123 tests passed.
- Production build: passed, 525 modules transformed.
- Administrator-surface policy covers all registered admin routes.
- Intranet deployment:
  `v0.0.1-20260724T143937Z-intranet-working-tree@2026-07-24T14:39:37Z`,
  healthy and ready.
- All 15 administrator route URLs returned HTTP 200. Runtime assets contain
  the shared surface, compact pager, refresh-button and overview-card markers;
  obsolete protocol-private surface markers are absent.

## Visual comparison

Final result: blocked.

The current independent Chrome administrator tab is unauthenticated. The
Codex built-in browser is intentionally not being driven or finalized because
its cleanup path is known to crash the application container.

Remaining authenticated checks after synchronization:

- page-level hierarchy at matching desktop and 390 px viewports for all 15
  administrator routes;
- status-card selection and count loading on `/admin/protocols`;
- chip dimensions and gaps on every list workbench;
- popover alignment, viewport clamping and focus return;
- ordinary, node-group and cursor-table alignment;
- active-chip truncation and clear affordance;
- compact table and cursor pagination;
- 390 px wrapping and horizontal-overflow behavior.
