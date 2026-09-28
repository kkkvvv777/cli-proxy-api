# Company Gateway Integration

This directory vendors the original CLI Proxy API Management Center, under its MIT license.
Upstream: https://github.com/router-for-me/Cli-Proxy-API-Management-Center
Baseline commit: `bbac79d2222a0f345458203a5ab92d859f30ff30` (retrieved 2026-09-22).

The original login, routing, theme, provider configuration, OAuth and API client are reused.
Company-specific changes are restricted to:

- `src/features/company/` and `src/services/api/company.ts`.
- A company-mode replacement of the shared API key field in configuration.
- An audit route and navigation entry in the same layout.
- The four existing locale files and focused regression tests.

Normal, non-company servers retain the original API key editor.
Company capability comes from the authenticated `/config` response. Employees and audit
use the same `apiClient` and management session; no second login or iframe is involved.
Employee changes persist immediately through `/company/users`, independently of YAML drafts.

Build using Bun 1.3.14:

```sh
bun install --frozen-lockfile
bun run verify
```

Install the generated `dist/index.html` as `management.html` in `MANAGEMENT_STATIC_PATH`.
Do not edit the generated HTML. `deploy/company/Dockerfile` builds and packages it automatically.
Company mode never auto-downloads or auto-updates an upstream panel over this build.
An old `/management.html?company=1` link redirects to the original config route's API key field.

Retain the upstream license when copying or redistributing this source.
