# Use server-rendered HTML with htmx for the MVP frontend

Accepted. Build the frontend as server-rendered HTML enhanced with htmx because the app is mostly forms, tables, dashboards, and partial updates rather than complex client-side state. This gives the cooperative a fast, snappy UI while avoiding SPA routing, hydration, frontend state management, and a separate JSON-client layer for the MVP.

**Considered Options**

- Server-rendered HTML plus htmx.
- Plain full-page HTML without partial updates.
- Vue or React as a single-page app.

**Consequences**

- UI endpoints should be able to return page fragments for htmx swaps.
- Forms should still be shaped like normal HTML forms where practical.
- Use small vanilla JavaScript only for behaviors htmx and HTML cannot express cleanly.
- Avoid mixing htmx with a half-SPA architecture unless a later ADR changes the frontend direction.
