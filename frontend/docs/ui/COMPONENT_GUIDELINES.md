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
- Use `FormField` for labels, hints, required state, and nearby errors.
  Pass its `controlAttrs` into the control.
- Use `UiSelect` for finite options and `UiAutocomplete` for remote choices.
  They use Reka keyboard, focus, and overlay behavior. Keep remote query state
  in the owning lookup component.
- Use `ModalDialog` for short forms and `DetailDrawer` for complex details.
  Both use Reka modal focus management. `ModalDialog` keeps dirty-form
  confirmation and explicit focus return.
- Use `StatusBadge` only for textual state. Choose a semantic tone and avoid
  several badges in one cell.
- Use `DataTable` and `TablePager` for data lists. Numeric cells align right;
  table identity stays visible at narrow widths. Use `EmptyState` and
  skeletons for first-load and empty states.
- Use `notify` for short operation feedback and `PageAlert` for page-level
  failures. Field validation stays at the field. Destructive operations use
  `confirmAction` and `ConfirmDialog`.

Avoid page-local control styles, `window.confirm()`, decorative shadows,
hover scaling, and ordinary text in the brand color.
