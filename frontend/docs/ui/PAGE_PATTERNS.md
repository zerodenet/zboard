# Page patterns

## Admin shell

The admin shell uses a quiet navigation tree. The current domain expands its
page links directly below the domain button. The topbar holds local page search,
location and account/site links; the page header owns the title, description,
and primary actions. At 820 px and below the sidebar becomes a focus-managed
drawer. The content column uses the available width with responsive padding.

## Data page

Use `PageHeader`, `WorkbenchFilterBar`, `DataWorkbench`, `DataTable`,
`TablePager`, and a `DetailDrawer` when quick inspection is useful.
List state belongs in the URL and server-side table reader. Keep current data
visible during refresh, and show a small refresh state. Separate initial
loading, empty results, API failure, and permission failure.

The node asset page is the reference data flow: it has search and filters,
selection, table status, detail drawer, edit forms, destructive confirmation,
pagination, and independent loading/error states. Keep business data and API
contracts in the page; reuse presentation and interaction through components.

## Settings page

Use a compact page header followed by titled sections, descriptions, fields,
and one action per section. Prefer dividers and whitespace to nested cards.
Field errors appear beside their controls.

## Detail page

Show the entity heading, status and metadata, tabs where needed, then content
sections. Use a sheet for quick inspection and an entire page for long
management tasks. Preserve return focus on close and Escape behavior.

## Verification

Review at 1920, 1440, 1280, 1024, and 768 px. Check normal, empty, loading,
refreshing, failed, forbidden, long text, and high-volume states. Verify
Tab, Shift+Tab, Enter, Space, Escape, and arrow-key paths for every overlay.
Build and type checks are necessary but do not replace this page review.
