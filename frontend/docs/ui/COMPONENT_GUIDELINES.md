# Component guidelines

The current application keeps `Ui*` entrypoints for existing pages. Those
entrypoints use native controls and Reka UI primitives; the button is sourced
from the official shadcn-vue Reka Nova registry at
`frontend/src/components/ui/button/`; Skeleton uses the same registry under
`frontend/src/components/ui/skeleton/`. Runtime application setup and test
fixtures no longer install PrimeVue, and it is absent from the package manifest.

- Use `UiButton` with `primary`, `secondary`, `ghost`, `danger`, or
  `link`. Use `loading` for mutations. Give icon-only buttons an accessible
  name and a tooltip when the action is not self-explanatory.
  Navigation actions use `UiButton as-child` around a `RouterLink` or `a`,
  retaining real anchor semantics. Do not style links with legacy `button`
  classes. Use size props instead of page CSS to change control dimensions.
- Use `FormField` for labels, hints, required state, and nearby errors.
  Pass its `controlAttrs` into the control.
  Prefill known values, choose the appropriate mobile keyboard, and focus the
  next required input. Do not explain an already clear control with extra prose.
  Do not reserve a visible blank help row when neither a hint nor an error is
  present. Keep the associated message ID and reveal its live error in place.
- Use `UiSelect` for finite options and `UiAutocomplete` for remote choices.
  They use Reka keyboard, focus, and overlay behavior. Keep remote query state
  in the owning lookup component.
  A filter commits only an explicit choice or clear action. Cancelling an
  unfinished keyword with Escape, blur or a closed popover retains its applied
  value. `UiAutocomplete` distinguishes explicit clears from unselected drafts;
  form lookups keep their existing forced-selection validation. Node/group
  lookups opt into `preserveSelectionOnCancel` when used inside filters.
- Use `ModalDialog` for short forms and `DetailDrawer` for complex details.
  Both use Reka modal focus management. `ModalDialog` keeps dirty-form
  confirmation and explicit focus return.
  Keep task dialogs focused on inputs and actions. Do not add implementation,
  draft-recovery or permission-mechanism paragraphs. Show short consequential
  warnings only when the relevant choice is made; keep errors near the action.
  A detail drawer already owns the entity title and metadata. Do not repeat
  them in a second heading inside the body. Keep frequent actions visible;
  place low-frequency actions in `RowActionMenu`, preserving confirmations.
  Detail context and actions form one compact toolbar, without another panel
  or separator. Keep tab names short and name the managed object. Routine
  instructions and implementation details belong in a closed disclosure;
  actionable failures and destructive consequences remain beside their action.
  The drawer body owns scrolling. Focusing a field or opening a menu must not
  scroll the outer sheet and move its title or close action out of view.
- Use `StatusBadge` only for textual state. Choose a semantic tone and avoid
  several badges in one cell.
- Use `BalanceValue` for read-only balances and `BalanceSummary` for grouped
  available/reserved balances. Zero and unavailable balances show `-`, with
  their actual meaning in accessible text. A group shows its currency once.
  Prices, payment confirmations, refunds and ledger changes keep explicit zero
  amounts; do not change their accounting or mutation formatters for decoration.
- `TableText` only becomes an expandable control when its rendered text is
  actually clipped. Fully visible content stays plain text, without a native
  title or duplicate popup. Measure available space after content, font and
  container-size changes; character count is not a layout test. Missing text and
  missing/invalid `TimeBadge` values use a plain `-`, without an idle button or
  clock icon. Omit absent optional secondary descriptions instead of adding an
  empty line beneath an entity name. Retain exact timestamps and diagnostics
  on valid values/hover.
- Hover and click must add information or perform a named action. Do not put
  the same complete content in the body, native title and popup. For truncated
  links/names use `v-overflow-title`; the link's real navigation remains intact.
  Use one tooltip for an icon action. Addresses expose a separate copy action;
  the address itself only expands when clipped. Keep exact-time, failure,
  currency and accessibility details when they add information.
- In a labelled table column, a binary `UiSwitch` already shows the editable
  state. Do not append a duplicate status badge or visible on/off label. Keep
  an accessible name, checked state and screen-reader description; explain
  distinct account states on hover and in details. Retain nearby visible errors
  and expose pending saves to assistive technology.
- Use `DataTable` and `TablePager` for data lists. Numeric cells align right;
  table identity stays visible at narrow widths. Use `EmptyState` and
  skeletons for first-load and empty states.
  When a monetary column has one currency, label the currency once in the
  header and show amounts in its cells. Mixed currencies retain per-amount
  identifiers. Preserve currency in accessible links and wallet navigation;
  do not infer or convert a record's currency from a display preference.
  Payment receipts use aligned amount, disposition, time and contextual action
  fields. Keep receipt/provider identifiers in transaction details, with a single
  wallet entry point for an already-paid top-up. Do not assemble records from
  nested generic stacks or repeat the same navigation in the payment summary.
  Entity names/IDs open details directly: use a real `RouterLink` for navigation
  or `EntityDetailButton` for an existing detail action. Keep the same URL state,
  permission checks and focus return as the existing action.
  Keep entity names directly navigable. In `RowActions`, the `primary` slot
  exposes frequent actions beside the menu; the default slot contains secondary
  actions. Compact tables should not require opening a menu to view or edit
  every entity. List search uses the capsule `WorkbenchFilterInput` entrypoint;
  show its input only after the user opens it.
  `WorkbenchEntityFilter` searches bounded server pages by name while retaining
  IDs in the URL. Put technical scope and granularity controls in advanced
  filters; never silently drop an invalid or unavailable saved ID.
- Use `notify` for short operation feedback and `PageAlert` for page-level
  failures. A repeated title/message is shown once; distinct failure reasons and
  recovery actions remain visible. Title-only alerts reserve no blank description.
  Field validation stays at the field. Destructive operations use
  `confirmAction` and `ConfirmDialog`.

Avoid page-local control styles, `window.confirm()`, decorative shadows,
hover scaling, and ordinary text in the brand color.

Setting rows show the named control and optional description, without an
additional on/off label or control-type badge. Keys, types, revisions and update
times belong in the closed technical-information disclosure. Do not render
“no description” placeholders. Keep conflicts and actionable errors visible.

Use at least 12px for descriptions, errors and lookup candidates; compact
metadata and chart axes may use 11px. Do not fit desktop content by shrinking
text to 8–10px. Adapt layout and wrap long values instead.

For teleported Reka popovers with scoped styles, use `PopoverContent as-child`
around a native styled content element. A scoped class on the Reka component
alone may not reach the teleported DOM. Verify computed styles in the browser,
including long unbroken text, available viewport width/height and scrolling;
the presence of a CSS rule in source is not visual acceptance.
The same viewport bounds apply to tooltips, select lists, autocomplete results
and action menus. Clamp their minimum width as well as maximum width, wrap long
unbroken values, and scroll inside the available height. Test both dark mode
and a narrow viewport.

Filter toolbars follow the project's Stripe-style capsule pattern. Suggested
conditions use dashed capsule triggers; active conditions show the selected
value and a clear action. Search is a capsule trigger too. Inputs and apply
buttons belong inside its popover, not permanently beside the capsules. Keep
this distinct from ordinary forms, where the standard input and button apply.
Do not replace capsule filters with rectangular secondary buttons or impose
ordinary button dimensions on them. Sharing code alone does not establish
Stripe design compliance.

Independent list fields may use a focused popover editor when a narrow mutation
API exists. Keep the read value aligned with its header; numeric sort icons go
before the label so the label and value share the same trailing edge. Click the
value to edit, focus/select the input, explicitly save or cancel, and restore
trigger focus on Escape. Do not save on blur or replace a full resource payload.
Reject invalid precision rather than silently rounding a billing value; retain
the draft and concrete error on failed saves. Disable duplicate pending writes
and reject late results after target, identity or permission changes.

`ProtocolMultiplierCell` uses the multiplier-only API for direct protocols.
Forward-entry multipliers are plain read-only text, without an editor, focus
stop or pointer action. Editing the parent protocol refreshes its related entry
rows without confusing their separate ID spaces.
For tables with 600px or less available width, action columns stop being sticky
so they cannot cover neighboring editable fields after horizontal scrolling.

Runtime charts use the shared section icon and compact segmented time-range
control. Keep percentage and network units on separate axes, a single vertical
inspection panel and aligned values. Use the component's available width for
layout. Keep chart axes and metadata at least 11px; never gain density by
shrinking text to 8–10px. Unknown samples break both lines and fills. Pointer,
touch and keyboard inspection must use the same timestamp across all series,
remain inside the chart and survive refresh without jumping to a newer point.

Administrative task targets show recognizable business identity directly: user
email first with an optional administrator note; node, protocol and group names;
subscription names with owner/plan information. Resolve only the requested page
in bounded batches. Do not make users read raw IDs or ORM names such as TaskItem
in the primary result column. Keep identifiers in closed technical details.
Deleted entities remain in task results with an explicit missing state; a failed
or unavailable lookup is not evidence of deletion. Avoid new retained copies of
personal data purely for display.


A table status cell shows one primary business result. Keep history, review and
retry controls in the fixed action column, never between status labels. Separate
independent states (such as subscription validity and automatic renewal) into
named columns; show only actionable exceptions for secondary processes. Do not
repeat a badge as adjacent explanatory text. Details belong in the relevant
record or editor, not an always-visible paragraph.

Task list, detail drawer and task tray use the same status projection. A finished
email task does not prove delivery: show server acknowledgment as submitted,
unresolved acknowledgment as needs review, and suppressed reminders as canceled.
Old records without acknowledgment remain unrecorded. Only offer review after
the execution lease is no longer live; review never sends a message. Pending
review must not offer an unconditional retry.


Console checks cover routed account pages as well as admin pages. Plain
attributes (protocol format, provider capabilities and billing method) are not
states and do not need status badges. Keep routine metric icons neutral and
omit empty description containers; reserve badge space only when a badge is
present. Use the same 400/500/600 weight scale in shared components and page
styles. Password confirmation errors belong at the confirmation field, while
server failures remain beside the form.
