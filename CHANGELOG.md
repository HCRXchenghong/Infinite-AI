# Changelog

## v2.0.0 - 2026-05-13

This release is tracked on GitHub at https://github.com/HCRXchenghong/Infinite-AI.

### Included

- Added Infinite Code as a formal Infinite-AI v2.0.0 product module. Infinite Code is the desktop and code-agent experience, while Infinite-AI provides shared account, authentication, model, subscription, quota, usage, telemetry, and admin-control services.
- Connected the Infinite Code desktop login, quota, billing, usage, model, and account flows to the Infinite-AI service layer.
- Added desktop-facing API support for account status, organization data, model availability, quota sync, and error reporting.
- Added admin-side model and quota management groundwork for controlling Infinite Code model access separately from the web app.
- Kept the GitHub repository as the canonical public handoff point for this version.

### Known Issues

- Agent capability is not fully stable yet. Tool execution, workspace continuity, archive/review flows, MCP, Skill, GitHub, and Gitee integration still need another full verification pass before they should be treated as complete.
- Frontend UI is functional but still needs a phase-two polish pass for density, spacing, responsive behavior, and visual consistency across user, admin, subscription, and desktop-related surfaces.
- v2.0.0 should be treated as a handoff/review version, not the final polished production build.
