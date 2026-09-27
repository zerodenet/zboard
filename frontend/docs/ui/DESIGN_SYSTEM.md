# ZBoard design system

The console uses a neutral, compact desktop density. The semantic colors and their
dark counterparts are in `frontend/src/theme/tokens.css`. Component and shared
page styles are in `frontend/src/theme/design-system.css`.

| Role | Token | Use |
| --- | --- | --- |
| Page | `--background`, `--foreground` | Canvas and main copy |
| Surface | `--card`, `--muted-surface` | Independent entities and quiet regions |
| Border | `--border`, `--input` | Section and control outlines |
| Action | `--primary`, `--primary-foreground` | One main action per area |
| Feedback | `--success`, `--warning`, `--info`, `--destructive` | Status with text |
| Focus | `--ring` | Visible keyboard focus |

Controls are 32 px compact, 36 px default, and 40 px large. Table rows are
40 px compact or 44 px comfortable. Controls use 6 px corners; floating menus
use 8 px and dialogs/cards use 10 px. Cards use borders, while overlays may use
a restrained shadow. Hover changes use 150 ms ease out. Reduced-motion users
receive near-instant transitions.

Use 24 px page titles, 16 px section titles, 13–14 px body text, and 12 px
secondary text. Keep ordinary headings, icons, and table content neutral.
Colors should describe state or identify the primary action, never decorate
an entire data region.

Page CSS may control layout and business visualizations. Reusable control
appearance belongs to the shared component style layer. Add new colors in
`tokens.css` and expose them with semantic names.
