# Use DESIGN-wise.md as the UI design system

Superseded by ADR 0010.

Accepted. Use `DESIGN-wise.md` as the source design system for the MVP UI so all admin and member pages share one visual language for color, typography, spacing, rounded corners, buttons, cards, forms, navigation, tables, dashboard summaries, and status badges. This avoids each implementation slice inventing its own UI style while keeping the interface operational and dashboard-oriented.

**Consequences**

- Frontend slices should map UI elements back to the design tokens and components in `DESIGN-wise.md`.
- The app should feel like a focused cooperative operations tool, not a marketing landing page.
- Any future design-system replacement should be recorded as a superseding ADR.
