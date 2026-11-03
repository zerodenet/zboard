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
- Use `ModalDialog` for short forms and `DetailDrawer` for complex details.
  Both use Reka modal focus management. `ModalDialog` keeps dirty-form
  confirmation and explicit focus return.
  Keep task dialogs focused on inputs and actions. Do not add implementation,
  draft-recovery or permission-mechanism paragraphs. Show short consequential
  warnings only when the relevant choice is made; keep errors near the action.
  A detail drawer already owns the entity title and metadata. Do not repeat
  them in a second heading inside the body. Keep frequent actions visible;
  place low-frequency actions in `RowActionMenu`, preserving confirmations.
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
  failures. Field validation stays at the field. Destructive operations use
  `confirmAction` and `ConfirmDialog`.

Avoid page-local control styles, `window.confirm()`, decorative shadows,
hover scaling, and ordinary text in the brand color.

For teleported Reka popovers with scoped styles, use `PopoverContent as-child`
around a native styled content element. A scoped class on the Reka component
alone may not reach the teleported DOM. Verify computed styles in the browser,
including long unbroken text, available viewport width/height and scrolling;
the presence of a CSS rule in source is not visual acceptance.

Filter toolbars follow the project's Stripe-style capsule pattern. Suggested
conditions use dashed capsule triggers; active conditions show the selected
value and a clear action. Search is a capsule trigger too. Inputs and apply
buttons belong inside its popover, not permanently beside the capsules. Keep
this distinct from ordinary forms, where the standard input and button apply.
Do not replace capsule filters with rectangular secondary buttons or impose
ordinary button dimensions on them. Sharing code alone does not establish
Stripe design compliance.
