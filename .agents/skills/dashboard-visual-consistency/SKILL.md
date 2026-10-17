---
name: dashboard-visual-consistency
description: Use when building or reviewing Cubeship dashboard pages, forms, tabs, actions, or visual consistency across resource workspaces.
---

# Dashboard visual consistency

Read `AGENTS.md` and `dashboard/AGENTS.md`. Before editing a screen, open the nearest screen doing the same job and its shared component. Compare Databases, Object Storage, and Backups when changing access, resource limits, or backups.

- Reuse the existing `SectionHeader`, `ActionButton`, `ConfirmDialog`, `CopyField`, `Notice`, `GlideSelect`, and form fields where they fit. Search call sites before changing a shared component.
- Keep sibling cards in the same section order, with `space-y-6` between cards and consistent title, description, field, hint, and action spacing. A warning that the confirmation dialog already explains does not belong on the resting page.
- Use the primary button for the section's main action, outline for secondary or reversal actions, and the same button size for peer actions. Put field submission below its field and hint in resource settings. Preserve busy and disabled behavior.
- Check the route, its workspace shell, and the closest sibling in the fixture preview (`make dashboard-preview`, port 3001) when visual changes need inspection. Do not confuse the fixture preview with the public demo build.
- Run Biome and the dashboard typecheck after TypeScript changes; run `make check` before committing as required by `AGENTS.md`.
