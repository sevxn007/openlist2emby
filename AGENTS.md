# Contributor and Agent Guidelines

[English](AGENTS.md) | [简体中文](AGENTS.zh-CN.md)

These instructions apply to the entire openlist2emby repository. Treat this
project as public open-source software: code, comments, tests, documentation,
examples, and history may all be reviewed by external contributors.

## Quality Bar

- Deliver complete, maintainable changes. Do not leave prototype code, dead
  branches, debug output, unexplained TODOs, or placeholder behavior.
- Prefer the smallest coherent change that solves the problem. Avoid unrelated
  refactors and formatting churn.
- Reuse existing helpers, components, and patterns before adding new ones.
- Do not duplicate domain logic between handlers, components, tests, or API
  clients. Extract shared logic only when it has a clear owner and real reuse.
- Avoid both rushed code and speculative architecture. Add an abstraction when
  it reduces current complexity, establishes a test boundary, or matches an
  existing project pattern.
- Keep functions and components focused. Split code when responsibilities,
  state, or failure handling become difficult to understand in one place.
- Use clear domain names. Do not hide behavior behind vague names such as
  `utils`, `helper`, `data`, or `process` when a specific name is available.

## Repository Boundaries

- `backend/` owns persistence, authentication, OpenList integration, external
  service integration, and the `/api/manager/*` contract.
- `frontend/` owns presentation and user interaction. It must not contain
  privileged credentials or bypass Manager APIs to call protected services.
- Keep API request and response types synchronized across backend and frontend.
- Preserve documented public interfaces unless a breaking change is explicitly
  approved. During pre-release development, persisted SQLite compatibility is
  not guaranteed; the schema in the current revision is authoritative.

## Go Backend

- Follow standard Go conventions and run `gofmt` on every changed Go file.
- Keep HTTP handlers thin: validate input, authorize, call a service/store, and
  serialize a stable response.
- Put external integrations behind small interfaces when doing so enables
  deterministic tests or isolates vendor-specific behavior.
- Propagate `context.Context` through request-scoped work. Set explicit network
  timeouts, close response bodies, and map upstream failures to stable Manager
  errors.
- Wrap errors with useful context, but never include credentials, session
  tokens, request authorization values, or secret-bearing URLs.
- Use parameterized SQL. SQLite schema setup must be deterministic and tested
  against a fresh database. Do not add compatibility migrations solely to
  preserve pre-release local data unless explicitly requested; document when an
  incompatible database must be backed up and recreated.
- Do not use `panic` for request or configuration failures. Return errors to the
  appropriate boundary.
- Prefer the standard library and existing dependencies. Add a dependency only
  when it removes meaningful maintenance burden and has an acceptable license,
  active maintenance, and a narrowly scoped use.

## React Frontend

- Keep TypeScript strict and avoid `any`, unsafe casts, and duplicated wire
  types. API access belongs in `src/shared/api`.
- Organize product behavior by feature. Keep app-shell composition separate from
  feature state and shared UI primitives.
- Use the existing component system consistently. Do not mix in another UI
  framework or add one-off primitives without explicit approval.
- Use TanStack Query for server state and mutations. Keep transient interaction
  state local to the smallest owning component.
- Derive values during render when possible. Do not mirror props or query data
  into state unless editing or reset behavior requires it.
- All visible product text must use the existing internationalization layer and
  include both English and Simplified Chinese translations.
- Do not add visible explanatory, instructional, or promotional copy unless the
  user explicitly requests it. Use concise labels and status messages required
  for the workflow.
- Preserve keyboard access, visible focus states, semantic controls, and touch
  targets of at least 44px.
- Verify desktop and mobile layouts. Dialogs, tables, long names, localized text,
  loading states, and error states must not overlap or cause horizontal overflow.

## Security and Privacy

- OpenList is the sole login credential authority for Manager and Emby; openlist2emby
  independently authenticates established local sessions. Never store
  passwords or hashes, implement local credential fallback, or use the OpenList
  administrator key as a substitute for user-session identity validation.
- Bind authorization by instance identity and immutable OpenList user ID, never
  by username. Keep Manager roles and library grants independent of upstream
  roles. Validate upstream identity at login, not on subsequent session requests.
  Check current local access and instance binding on every authenticated request.
  Renew browser sessions for 24 hours and Emby sessions for 30 days on successful
  authentication; never restore expired or revoked sessions. Upstream account
  changes do not revoke existing local sessions automatically.
- Use OpenList user tokens only server-side during login; do not persist new
  ones. Emby does not support two-factor accounts. Directory operations remain Manager-admin-only and use the configured
  integration key. Develop against the current fresh-database schema; do not add
  legacy identity migration or compatibility paths. Never clear runtime data automatically.
- Never commit real API keys, tokens, passwords, cookies, personal endpoints,
  customer data, or local database contents.
- Keep secrets server-side. Do not expose them in API responses, logs, errors,
  browser storage, test fixtures, screenshots, or frontend bundles.
- Use placeholders in examples and `.env.example` files.
- Keep runtime databases, build output, dependency directories, coverage output,
  and local IDE files ignored by Git.
- Use synthetic credentials in tests. Tests that exercise a real external
  service must be opt-in and read credentials from the environment.
- Do not copy third-party source or assets without checking license compatibility
  and preserving required attribution.

## Testing

- Add tests proportional to the behavioral risk of the change.
- Avoid over-testing. By default, run only the smallest relevant test targets
  and static/build checks needed to validate the files and behavior changed.
- Do not repeatedly run the same successful check unless subsequent edits could
  have invalidated it. Do not use browser or end-to-end verification when unit,
  component, API, or build checks provide sufficient evidence.
- Backend tests should cover current-schema initialization, authorization,
  validation, secret masking, upstream mapping, and failure status codes where
  applicable.
- Frontend tests should cover API payloads, rendering states, core interactions,
  generated values, internationalization behavior, and regressions in the main
  workflow.
- Unit and component tests must be deterministic. Use fakes or local test servers
  instead of relying on live OpenList or third-party services.
- Run the complete verification suite only for broad or cross-cutting changes,
  release preparation, changes with a large blast radius, or when the user
  explicitly requests it:

  ```bash
  cd backend
  go test ./...
  go vet ./...

  cd ../frontend
  pnpm lint
  pnpm test
  pnpm build
  ```

- For focused changes, run only the affected package or test file plus the
  necessary compile, lint, or build check. State which checks were intentionally
  omitted when that information is relevant to the handoff.
- Do not claim a check passed unless it was actually run. If a check cannot run,
  report the exact reason.

## Documentation and Public Interfaces

- Update documentation when changing setup, environment variables, API routes,
  database behavior, or contributor workflows.
- Document exported APIs and non-obvious invariants, not self-explanatory code.
- Keep comments concise and explain why a constraint exists rather than narrating
  what each line does.
- Return stable JSON envelopes from Manager APIs and never silently change field
  meaning or secret-preservation behavior.

## Git and Review Hygiene

- Keep commits focused and reviewable. Use Conventional Commits in the form
  `type(scope): imperative subject`.
- Do not commit generated frontend builds, runtime databases, dependency caches,
  test reports, or unrelated local changes.
- Review the complete diff before handoff. Check for accidental secret exposure,
  duplicated logic, stale code, missing translations, and untested behavior.
- Do not rewrite or discard another contributor's changes without explicit
  approval.
- If issue or pull-request templates are added, follow them exactly and never
  invent validation results, logs, screenshots, or reproduction details.

## Definition of Done

A change is complete only when its behavior is implemented end to end, relevant
proportional checks are passing, secrets are absent from tracked and generated
files, public contracts remain coherent, and the code is understandable without
relying on private conversation context.
