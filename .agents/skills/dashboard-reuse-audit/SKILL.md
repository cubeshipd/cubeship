---
name: dashboard-reuse-audit
description: Use when auditing Cubeship dashboard for dead components, duplicated UI, or chances to consolidate visual patterns.
---

# Dashboard reuse audit

Start at actual Next routes and follow their component imports. Search references across the repository before deleting a file or export; a component in `src/components` is not necessarily used. Distinguish dead UI from API types, fixture handlers, and server contracts that still have consumers.

For duplication, compare rendered structure and behavior in sibling workflows. Share a component when at least two call sites have the same layout and interaction contract, keeping their API calls, permissions, error handling, and resource-specific text in the owners. Existing shared primitives are preferable to another wrapper.

After removing or consolidating code, inspect the diff for orphaned imports, CSS selectors, fixtures, and props. Run Biome and the dashboard typecheck; use `make check` before committing. Report what was removed, what was consolidated, and any visually unverified surfaces separately.
