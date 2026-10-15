# openlist2emby Backend

The backend is a standalone Go service for one OpenList instance. It owns OpenList identity bindings,
Manager authentication, SQLite persistence, OpenList administrator access,
TMDB, TVDB, and music integrations, media-library indexing, Emby-compatible APIs,
durable task execution, and the `/api/manager/*` HTTP contract.

## Architecture

Dependencies point inward: HTTP transport calls application services, domain
and task modules define behavior, and SQLite or external clients implement the
required ports.

```text
cmd/server/                     composition root and process lifecycle
internal/
  domain/                       core models and domain errors
  app/                          setup, settings, proxy, media, and music use cases
  medialibrary/                 library models, indexing, scanning, and playback resolution
  emby/                         Emby API, routing, playback, and GORM-backed device state
  task/                         task registry, scheduler, workers, and state machine
  repository/sqlite/           SQLite stores and schema management
  audio/tagger/                 in-process cover and lyrics writer
  client/music/                 music search, cover, and lyrics adapter
  client/openlist/              OpenList HTTP and file-transfer adapter
  client/tmdb/                  TMDB adapter and response mapping
  client/tvdb/                  TVDB adapter and response mapping
  metadata/                     source selection, priority, and query matching
  transport/httpapi/            routing, session middleware, DTOs, and static files
  config/                       flags and environment configuration
```

`cmd/server` wires the store, clients, application service, task registry,
task manager, and HTTP server. `internal/app` owns request-level use cases.
`internal/task` owns persistent task behavior without depending on HTTP or
SQLite. `internal/repository/sqlite` implements both application and task
storage contracts.

## OpenList Identity And Local Authorization

OpenList is the only credential authority. The identity client wraps login,
current-user validation, and administrator user lookup with a 10-second timeout,
redirect rejection, bounded responses, and sanitized errors. OpenList user tokens
are used only server-side during login and are not persisted for new sessions.
No account passwords/hashes are stored.

Manager roles (`admin`, `media`), enabled access, and library grants are local
permissions, not copied from upstream roles. Bindings use instance ID + OpenList
user ID, never username. New identities are denied until explicitly authorized.
The directory proxy, tasks, settings, and authorization management remain
administrator-only; media users get only their granted libraries and operations.

Browser sessions have a sliding 24-hour lifetime; Emby sessions have a sliding
30-day (30×24-hour) lifetime. Every successful local authentication extends expiry
to the current time plus that lifetime without shortening an existing expiry.
Authenticated browser responses refresh the HTTP-only session cookie to match.
Expired or deleted sessions require login again and are never restored.
There is no background keepalive or OpenList JWT refresh.

OpenList login and `/api/me` establish identity only at login. Subsequent requests
validate the local session type, expiry, enabled access, and instance binding,
and reload local roles and grants without contacting OpenList for authentication.
OpenList token expiry or outages do not invalidate established sessions. Actual
upstream resource operations can still fail and retain their existing permissions.
OpenList password changes, account deletion/disablement, and OTP changes do not
automatically revoke existing local sessions; use local disablement, access
revocation, or device logout for immediate revocation. Names refresh on next login.
Browser login accepts `otp_code`; Emby rejects two-factor accounts at login.
Device logout revokes its linked identity session. No passwords or password hashes
are retained, and new logins still fail when OpenList authentication is unavailable.
Identity sessions store local authorization and expiry state, not OpenList user tokens.

### Fresh Installation

With no settings row, `/api/manager/status` returns `setup_required: true`.
`POST /api/manager/setup` accepts:

```json
{
  "openlist_base_url": "https://openlist.example.invalid",
  "openlist_admin_key": "<administrator-key>",
  "username": "<existing-openlist-administrator>",
  "password": "<openlist-password>",
  "otp_code": "<optional-verification-code>"
}
```

Both the key and login must identify an enabled OpenList administrator. Setup
atomically stores settings, instance identity, and the first authorized admin;
it does not create accounts. Repeated setup is rejected.

### Manager Authentication And Authorization APIs

Responses retain the `{code,message,data}` envelope.

- `POST /api/manager/auth/login`: `username`, `password`, optional `otp_code`.
  Returns user information and an HTTP-only `openlist2emby_session` cookie,
  never an OpenList token. Authentication failure is 401, missing openlist2emby access
  403, upstream unavailability 503, and upstream throttling 429.
- `POST /api/manager/auth/logout`: deletes the session and expires its cookie.
- `GET /api/manager/users`: authorized and retained unbound records, including
  independent `enabled` and upstream `active`/`disabled`/`unavailable`/`unbound`
  status. Missing upstream users are reported as unavailable.
- `GET /api/manager/openlist/users?page=1&per_page=50`: administrator-only,
  paginated whitelist (`id`, `username`, `role`, `disabled`, `otp`), with `content`
  and `total`. Page size is 1–100. The upstream list may omit OTP status; only
  `/api/me` is authoritative for login two-factor policy.
- `POST /api/manager/user-authorizations`: administrator-only; accepts
  `openlist_user_id`, `role`, `enabled`, `library_grants`. Duplicate bindings are
  rejected. Media roles require at least one library grant.
- `PUT /api/manager/users/{id}`: only `role`, `enabled`, `library_grants`; rejects
  all unknown fields, including retired `username`/`password` even if null.
- `POST /api/manager/users` and `DELETE /api/manager/users/{id}` are retired JSON
  404 endpoints. Account CRUD belongs in OpenList.

Local access revocation deletes identity sessions and revokes Emby credentials.
Role/library changes apply on the next request. The last available bound admin
cannot be disabled or demoted; admins cannot disable their own access. Temporary
upstream failure blocks availability-sensitive authorization updates.

## OpenList Integration Access

Filesystem operations, metadata jobs, scans, and playback URL resolution use the
configured server-side administrator key. User tokens are never used to extend
ordinary users' directory privileges. Responses expose only masked keys; blank
secret updates preserve existing values. An online base-URL change is rejected
and requires a fresh installation for the different instance.

## SQLite Schema

The default database is `./data/openlist2emby.db`. Startup initializes the
current schema for new installations. Use an empty database and metadata
directory. Pre-release schema compatibility is not guaranteed, and startup
never automatically deletes runtime data.

The task subsystem uses:

- `tasks` for reusable task definitions, payload snapshots, optional
  Cron/timezone settings, `next_run_at`, ownership, and soft deletion.
- `task_runs` for manual and scheduled queue entries, progress, attempts,
  leases, cancellation, errors, results, and timestamps.
- `task_run_items` for ordered per-object actions, progress, upstream task
  state, safe errors, and timestamps. Rows are removed with their task run.

Media-library and Emby compatibility use:

- `settings.media_library_enabled` as the opt-in feature switch.
- `media_item_images`, `media_people`, and `media_item_people` for localized artwork,
  Chinese people metadata, and item-credit relationships.
- `media_library_roots` for the ordered root folders in each media library.
- `media_libraries` and `media_items` for movie, TV show, and music indexes.
- `identity_provider`, `user_authorizations`, `sessions`, and `user_library_grants` for identity bindings,
  active Manager sessions, and view/edit media-library authorizations.
- `media_user_data` for each user's resume position, played state, play count,
  and favorites.
- `emby_tokens` for hashed, device-specific access tokens and `emby_state` for
  the stable server identifier.

The `emby` package owns its SQLite models and initializes those two Emby tables
through the shared GORM session. Application, task, media-library, and Emby
persistence use GORM with a pure-Go SQLite driver. Raw SQLite expressions are
limited to connection PRAGMAs and the recursive media-item hierarchy query.

SQLite runs with foreign keys enabled, a 5-second busy timeout, WAL journaling,
and one database connection per Manager process. The design targets one Manager
process with a local SQLite file, not a multi-node deployment.

## Persistent Task Module

### Handler Registry

Only task handlers compiled into the Go registry can run. Arbitrary scripts and
arbitrary HTTP tasks are not supported. Each handler implements:

- `Descriptor`: type name, payload version, and manual, schedulable, and
  cancelable capabilities.
- `Prepare`: strict validation and normalization before a payload is persisted.
- `Run`: cancellable execution with a throttled progress reporter.
- `PublicSummary`: a safe API view that avoids returning secret-bearing raw
  payloads.

The registry currently contains three payload-version-1 handlers:

- `music.embed_metadata` is manual and cancelable, but not schedulable.
- `media.scan` is manual, schedulable, and cancelable.
- `media.metadata.relocate` is manual and cancelable, but not schedulable.

### State And Delivery Semantics

The persisted state machine is:

```text
queued -> running -> succeeded | failed | canceled
             |
             `-> queued (automatic retry or interrupted-worker recovery)
```

Execution is at least once. Handlers must tolerate running the same persisted
payload again after an interruption. A run normally allows three attempts: the
initial attempt plus two automatic retries. Retryable network failures,
timeouts, upstream HTTP 408/429 responses, and upstream 5xx responses use
backoffs of 30 seconds and then 2 minutes. Validation, unsupported media, and
other non-retryable errors fail immediately.

Queued cancellation is immediate. Running cancellation records a request and
cancels the in-process run context; it remains cooperative and does not forcibly
terminate a Go routine or an in-progress local write. Failed and canceled runs
can be retried manually only while their task type remains registered, producing
a new run linked by `retry_of_run_id`.

### Workers And Leases

- The default worker pool has 2 workers and can be configured from 1 to 32.
- Claiming a run atomically changes it from `queued` to `running` and assigns a
  60-second lease.
- The worker renews its lease every 15 seconds and checks cancellation at the
  same boundary.
- On startup and during execution, expired leases are recovered. A run with
  attempts remaining returns to the queue after 30 seconds; an exhausted run
  becomes `failed`.
- Graceful shutdown stops HTTP intake, cancels workers, and releases active runs
  back to the queue without consuming an attempt.
- Progress writes are throttled to at most twice per second unless the stage
  changes or reaches 100 percent.

Closing a browser or disconnecting an SSE request only stops that client from
waiting; it does not cancel the persisted run.

### Cron Scheduling

Definitions without a Cron expression are manual tasks and are never selected
by the scheduler. Any saved definition can be queued once through its `/run`
endpoint. Adding a Cron expression and timezone to a manual definition converts
the same record into a schedule.

Schedules use standard five-field Cron expressions:

```text
minute hour day-of-month month day-of-week
```

Seconds and `@every` descriptors are not enabled. Each schedule also requires a
valid IANA timezone such as `UTC` or `Asia/Shanghai`. `next_run_at` is persisted
in UTC.

The scheduler advances a due schedule directly to the next occurrence after
the current time. This produces at most one catch-up run after downtime. Each
schedule may have at most one running run and one queued run; additional due
ticks are coalesced while that queued run exists. Workers will not execute two
runs from the same schedule concurrently.

Disabling a schedule cancels its queued run. Deleting a schedule soft-deletes
the definition and cancels queued work; an already running run is allowed to
finish.

### Retention

Terminal runs are deleted after 30 days by default. Cleanup runs when the task
manager starts and then every 24 hours. Task definitions are retained until they
are soft-deleted. Change the terminal-run retention window with
`OPENLIST2EMBY_TASK_RETENTION_DAYS` or `-task-retention-days`.

### Music Metadata Task

Music metadata providers are registered behind a shared provider interface.
NetEase Music, QQ Music, and Kuwo currently implement the same search, cover,
and lyrics contract. Library scans query all registered providers concurrently,
then rank the combined results by normalized title, artist, and album matches;
equal scores retain the provider registration order. Adding another provider
does not require changing the scan matcher or the Manager music response types.

`music.embed_metadata` accepts a music-library audio item ID. The backend
validates the item and resolves its current OpenList media path before storing
the path, source identifiers, embed choices, and an OpenList base-URL snapshot.
It never stores the OpenList
administrator key or a Manager session token in the task payload.

At execution time the handler reads the current administrator key. It rejects
the run as non-retryable if the configured OpenList address changed after the
run was created. Otherwise it downloads the audio file, writes selected cover
art and lyrics into the native container tags, and uploads the result to the
same path with overwrite enabled.

`POST /api/manager/music/embed` remains compatible with its original JSON and
SSE responses. Internally it creates the same persistent task and waits for its
result. A disconnected request does not cancel the task.

### Media Metadata Relocation Task

`media.metadata.relocate` moves the local metadata cache to the directory chosen
in Settings. It reports progress through the existing task engine and keeps
media-library artwork and metadata access coordinated with the move. This is a
filesystem operation, not a SQLite schema migration.

### Media Library Scan Task

Without selected paths, `media.scan` recursively indexes every OpenList root in a media
library as movies, TV shows, or music. A library must have at least one root;
its roots must be absolute, unique, and non-overlapping. Full scans allow cached
OpenList directory listings. While discovering a large
tree, the task reports cumulative scanned directory and file counts even though
the indexing percentage cannot be known yet. It then reports percentage progress
while indexing supported files.

Administrators can submit selected paths to
`POST /api/manager/media-libraries/{id}/import`, for example
`{"paths":["/TV/Show/Season 01/Show.S01E02.mkv","/TV/Another Show"]}`.
The nonempty `paths` array accepts up to 1000 absolute paths within the target
library's roots. Duplicate or overlapping selections are collapsed, and current
root boundaries are checked again when the task runs. Incremental imports refresh
only selected subtrees or file parents while retaining the original library-relative
hierarchy. They reuse identification, metadata, and indexing without advancing the
full-scan generation, deleting unselected items, or reassociating identities based
on presumed missing paths. Screenshots cover only affected media and new parents;
global asset cleanup is skipped. Full scans still reconcile deleted files.
Manual scans, scheduled scans, and imports for the same library are claimed
serially; different libraries can run concurrently. Task subjects include `paths`,
and closing the browser does not interrupt a queued import.

Movie and TV show scans parse titles, seasons, and episodes and enrich them with
the configured TMDB or TVDB source. Existing items reuse stored
metadata until the source file modification time or source selection changes; manually edited
metadata stays locked during later scans. Music scans index each audio file as an individual song. A successful full scan commits a new
generation and removes items no longer present. Its result records added,
updated, removed, directory, file, metadata-failure, screenshot-success, and
screenshot-failure counts. When a movie or TV library enables video screenshots,
the scan adds a `screenshots` progress stage after metadata localization and
uses no more than two concurrent frame captures. Individual capture failures do
not fail the scan.

Each movie or TV library may enable `hide_people_without_images`. When enabled,
Manager and Emby people responses omit cast and crew without a cached local
profile image (localized mode) or a TMDB profile URL (remote mode). The setting
only changes presentation and does not remove people or relationships.
If TMDB has no still image for an episode, scans use the parent series backdrop
as the episode's primary cover until a later metadata refresh supplies a still.

Each media library owns exactly one `media.scan` schedule. New libraries create
an enabled `0 * * * *` schedule in `UTC`; administrators can change its Cron
expression, timezone, or enabled state through the media-library API. Deleting
a library soft-deletes its schedule and cancels queued runs. The schedule is created with its library in the current schema.

Remote metadata sources are used only while scanning, searching, or explicitly refreshing metadata.
Chinese media text, people relationships, posters, backdrops, episode stills,
and profile images are persisted locally. Browse, season/episode, `PersonIds`,
and Emby image requests do not contact metadata providers. The default cache is the `media`
directory beside the SQLite database (`/data/media` in Docker).

Person local works (`GET /api/manager/media-library/items?person_id=…` and
Emby `PersonIds`) use persisted local relationships, without remote filmography
fallbacks. A series or season credit returns the series. An episode credit
returns the episode, except an Actor credit is suppressed when that same person
is also an Actor on the series or any of its seasons. Explicit non-Actor episode
credits remain visible. Movie credits return movies. Multiple people match with
OR semantics; deduplication happens before counting, sorting, and pagination.
Library permissions, parent scope, and item-type filters remain strict: Emby
`IncludeItemTypes=Series` never includes guest episodes, and `Episode` never
includes series or suppressed regular-cast episodes. Emby callers querying across
a hierarchy should pass `Recursive=true`; non-recursive parent scope is not
expanded implicitly for person queries.

Manager episode list entries may include `series_id` and `series_name`; `name`
remains the episode title. Person works can contain movie, series, and episode
cards and support loading additional pages. Missing local credits require the
existing metadata refresh workflow; opening a person does not refresh metadata.
Roles merged into an Actor relationship follow Actor rules; role text is not parsed to infer duties.

The payload stores the media-library ID and an OpenList base-URL snapshot, not
the administrator key. The handler reads the current key when it runs and
rejects execution if the feature is disabled or the OpenList address changed.

## Media Library And Emby Compatibility

The media library is opt-in through `media_library_enabled` in Manager settings
and is disabled by default. When disabled, Manager media-library calls and all
Emby-compatible paths return HTTP 404. When enabled, authenticated Manager APIs
create libraries, browse indexed items, start `media.scan` runs, and open media
through an HTTP 302 redirect.

The recommended Emby server address is `<openlist2emby-origin>/emby`. The
server also recognizes `/mediabrowser` and common unprefixed Emby paths for
client compatibility. Emby clients authenticate with an authorized OpenList
account, but receive a dedicated random device token rather than a Manager
session cookie or the OpenList administrator key. The database stores only the
token hash; logout revokes that token.

`POST /Users/{UserId}/Items/{Id}/HideFromResume?Hide=true|false` hides or
restores an item for the authenticated user. The per-user flag is persisted;
resume and continuation queries exclude hidden candidates without clearing
playback position, watched state, play counts, favorites, or history. A hidden
next episode does not silently advance the series to a later episode.

Playback responses advertise direct streaming and explicitly disable
transcoding. Each video or audio stream request resolves the indexed media path
through a fresh OpenList `/api/fs/get` call, then redirects the client to the
returned `raw_url` with HTTP 302 and `Cache-Control: no-store`. openlist2emby never proxies the media body and does not transcode it.

The Manager media-library routes are:

| Method                 | Path                                                           | Purpose                                                                                                  |
|------------------------|----------------------------------------------------------------|----------------------------------------------------------------------------------------------------------|
| `GET`, `POST`          | `/api/manager/media-libraries`                                 | List visible libraries or create one; administrators may use `include_hidden=true` to list every library |
| `GET`, `PUT`, `DELETE` | `/api/manager/media-libraries/{id}`                            | Read, replace, or delete one library                                                                     |
| `GET`, `PUT`           | `/api/manager/media-libraries/{id}/scan-schedule`              | Read or replace the library's unique Cron scan schedule                                                  |
| `POST`                 | `/api/manager/media-libraries/{id}/scan`                       | Queue a full `media.scan` run                                                                            |
| `POST`                 | `/api/manager/media-libraries/{id}/import`                     | Queue an incremental `media.scan` run for selected absolute `paths` (administrator only)                 |
| `GET`                  | `/api/manager/media-library/items`                             | Search or browse indexed items                                                                           |
| `GET`                  | `/api/manager/media-library/items/{id}`                        | Read one indexed item                                                                                    |
| `GET`                  | `/api/manager/media-library/continue-watching`                 | Merge real resume items with one dynamically derived next episode per started series                     |
| `GET`                  | `/api/manager/media-library/items/{id}/next-episode`           | Resolve the next episode for an episode, season, or series                                               |
| `PUT`                  | `/api/manager/media-library/items/{id}/played`                 | Mark an item watched or unwatched while preserving unrelated user state                                  |
| `PUT`                  | `/api/manager/media-library/items/{id}/resume-visibility`      | Set `{ "hidden": true/false }` without clearing playback progress; shared with Emby                      |
| `DELETE`               | `/api/manager/media-library/items/{id}/resume`                 | Clear only the real resume position                                                                      |
| `PUT`                  | `/api/manager/media-library/items/{id}/metadata`               | Replace manually edited item metadata (administrator only)                                               |
| `POST`                 | `/api/manager/media-library/items/{id}/metadata/refresh`       | Refresh movie or series metadata using `metadata_source`, `tmdb_id`, and `tvdb_id` (administrator only)  |
| `PUT`                  | `/api/manager/media-library/items/{id}/images/{type}`          | Upload a primary or backdrop image (administrator only)                                                  |
| `POST`                 | `/api/manager/media-library/items/{id}/images/primary/capture` | Capture a video frame as the primary image (administrator only)                                          |
| `GET`                  | `/api/manager/media-library/items/{id}/stream`                 | Resolve a fresh OpenList URL and return HTTP 302                                                         |

Library create and update requests use `root_paths` in the configured scan
order. The `root_path` field accepts a single path and is returned as the first
path. Movie and TV requests may also set
`video_screenshot_enabled` and `video_screenshot_percent`; creation defaults to
`false` and `10`, omitted update fields preserve their current values, and music
libraries always disable the feature. The percentage must be from 1 to 99.
`playback_completion_percent` controls the stopped-playback threshold for
marking items played; it defaults to `90`, is valid from 1 to 100, and omitted
updates preserve the current value.

Video capture uses `ffprobe` to read duration and FFmpeg to write a bounded JPEG
at the configured percentage. Both executables must be available on `PATH` for
native deployments. Their absence does not prevent startup, but enabling the
feature or manually capturing a frame returns HTTP 503. Signed OpenList URLs
and FFmpeg command output are never included in API errors.

```json
{
  "name": "Movies",
  "content_type": "movies",
  "root_paths": ["/Media/Movies", "/Media/Archive"]
}
```

## Task APIs

Every task endpoint requires a valid Manager identity session cookie. Responses use
the standard Manager envelope:

```json
{
  "code": 200,
  "message": "success",
  "data": {}
}
```

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/manager/tasks/types` | List registered handler descriptors |
| `GET` | `/api/manager/tasks/schedules` | List non-deleted task definitions and their latest run; optionally filter by `type` |
| `POST` | `/api/manager/tasks/schedules` | Create a manual or scheduled definition; returns HTTP 201 |
| `GET` | `/api/manager/tasks/schedules/{id}` | Read one task definition |
| `PUT` | `/api/manager/tasks/schedules/{id}` | Replace one task definition |
| `DELETE` | `/api/manager/tasks/schedules/{id}` | Soft-delete a task definition |
| `POST` | `/api/manager/tasks/schedules/{id}/run` | Queue one immediate run; returns HTTP 202 |
| `GET` | `/api/manager/tasks/runs` | Filter and paginate run history |
| `POST` | `/api/manager/tasks/runs` | Queue a manual run; returns HTTP 202 |
| `GET` | `/api/manager/tasks/runs/{id}` | Read run state, progress, result, or safe error |
| `DELETE` | `/api/manager/tasks/runs/completed` | Admin-only: delete all succeeded and canceled runs across all pages and types, ignoring list filters; returns `{ "deleted": count }` (zero when none match). Queued, running, and failed runs are retained. |
| `DELETE` | `/api/manager/tasks/runs/{id}` | Delete a queued or terminal run; running runs return HTTP 409 |
| `GET` | `/api/manager/tasks/runs/{id}/items` | Filter and paginate ordered per-item progress |
| `POST` | `/api/manager/tasks/runs/{id}/cancel` | Request cancellation |
| `POST` | `/api/manager/tasks/runs/{id}/retry` | Queue a new run from a failed or canceled run; returns HTTP 202 |

Run-list filters are optional:

```http
GET /api/manager/tasks/runs?status=queued,running&type=music.embed_metadata&page=1&per_page=20
```

`page` defaults to 1, `per_page` defaults to 20, and the maximum page size is
100.

Run-item filters accept comma-separated `status` and `action` values. The item
response includes filtered pagination totals and an unfiltered summary for the
entire run.

Create a manual music metadata run with:

```json
{
  "type": "music.embed_metadata",
  "payload": {
    "item_id": 42,
    "source": "netease",
    "picture_id": "<picture-id>",
    "lyric_id": "<lyric-id>",
    "embed_cover": true,
    "embed_lyrics": true
  }
}
```

Task-definition create and update requests share this generic shape:

```json
{
  "name": "Example schedule",
  "type": "<registered-schedulable-type>",
  "cron_expression": "0 3 * * *",
  "timezone": "Asia/Shanghai",
  "enabled": true,
  "payload": {}
}
```

For a manual definition, send empty `cron_expression` and `timezone` values and
set `enabled` to false. The backend also forces these values when Cron is empty.

Public schedule responses expose `parameters`, and public run responses expose
`subject`; both come from the handler's `PublicSummary`. Raw persisted payloads,
administrator keys, and session tokens are not returned.

## Other API Areas

- Status and setup: `/api/manager/status`, `/api/manager/setup`
- Local sessions: `/api/manager/auth/login`, `/api/manager/auth/logout`, and
  `/api/manager/auth/me`
- Settings and connection test: `/api/manager/settings*`
- TMDB media operations: `/api/manager/media/*`
- Music search and embedded tags: `/api/manager/music/*`
- Media-library management and playback redirects:
  `/api/manager/media-libraries*` and `/api/manager/media-library/*`
- OpenList filesystem proxy: `/api/manager/openlist/fs/*`
  - `POST /api/manager/openlist/fs/mkdir` (Manager admin only): forwards `{ "path": "/Media/New folder" }` to OpenList `/api/fs/mkdir` using the server-side integration key. Returns the existing proxy JSON envelope; no overwrite or automatic renaming is applied.

## Configuration

From `backend/`:

| Flag | Environment variable | Default | Validation |
| --- | --- | --- | --- |
| `-addr` | `OPENLIST2EMBY_ADDR` | `127.0.0.1:8091` | Go HTTP listen address |
| `-db` | `OPENLIST2EMBY_DB` | `./data/openlist2emby.db` | SQLite path |
| `-static` | - | empty | Existing frontend `dist` directory |
| `-task-workers` | `OPENLIST2EMBY_TASK_WORKERS` | `2` | Integer from 1 through 32 |
| `-task-retention-days` | `OPENLIST2EMBY_TASK_RETENTION_DAYS` | `30` | Positive integer |

Flags override their environment-backed defaults. Relative database and static
paths are resolved from the process working directory.

## Development And Verification

The default development listener is `http://127.0.0.1:8091`. Vite runs on
`http://127.0.0.1:5174` and proxies `/api/manager/*` requests to that listener.
Emby clients use the separate listener configured in Settings.

```bash
go run ./cmd/server
go test ./...
go test -race ./...
go vet ./...
```

To serve a previously built frontend and the API on one port from `backend/`:

```bash
go run ./cmd/server -static ../frontend/dist
```

The HTTP server serves frontend assets for non-API paths and falls back to
`index.html` for client-side routes.

### Metadata sources and release years

Metadata settings support TMDB and TVDB credentials and an ordered source list
(default TMDB, then TVDB). Empty credential inputs preserve stored values;
explicit clear flags remove them. Credentials remain server-side and responses
are masked. TVDB uses its official API with an optional subscriber PIN and Aired
Order. Configure credentials licensed for your project before using the source.

Movies and series expose `tmdb_id`, `tvdb_id`, `metadata_source`
(`auto`, `tmdb`, or `tvdb`) and `resolved_metadata_source`. Automatic selection
uses the first unambiguous match from configured sources. Authentication,
network and rate-limit failures stop selection; they do not silently replace
existing metadata using another source. Provider data uses Chinese when
available, with original-language fallback.

Search accepts a title with a delimited release year, such as `Title (2006)`,
`Title（2006）`, `Title[2006]` or `Title.2006`. The title is searched separately
and results are filtered by release year (first-air year for series). A year
without a title remains a title query. Scanning uses the same year parser.

`POST /api/manager/media-library/items/{id}/metadata/refresh` accepts optional
`tmdb_id`, `tvdb_id` and `metadata_source`. IDs saved through the metadata edit
endpoint are used when omitted. Library edit authorization remains required.
Provider failures retain existing metadata; manual artwork and locked fields
are preserved. Emby retains Episode presentation and exposes genuine provider
identifiers. Season numbers may contain one through four digits.

Metadata binding writes and the `classic_short_order` refresh parameter are
unsupported and rejected. Source IDs are never inferred from file order.
Schema initialization includes source fields and person identities without
clearing runtime data.
Metadata and artwork obtained from TMDB and TheTVDB remain subject to their
respective attribution and licensing requirements.

### Image selection

Only administrators can edit metadata, upload/capture images, or search/select
remote images. Library grants authorize browsing and playback only; an `edit`
grant does not enable editing. `editable_library_ids` remains an empty array.

- `GET /api/manager/media/image-sources` returns `{sources: [...]}` in configured
  priority order without credentials.
- `GET /api/manager/media/images` takes `source` (`tmdb` or `tvdb`), `type`
  (`movie` or `tv`), `id` (movie or **series** source ID), `image_type`
  (`primary` or `backdrop`), and optional `season`/`episode` numbers. It returns
  `{images: [...]}` with `source`, `image_type`, `url`, `thumbnail_url`, and
  optional `width`, `height`, `language`. No supported artwork yields an empty
  array; TV child requests do not fall back to series artwork.
- `POST /api/manager/media-library/items/{id}/images/{type}/select` takes the
  same source identity fields and a chosen `url`. The server verifies that the
  URL is still in the provider candidate list, then replaces only the selected
  image through the existing asset localization flow. The JSON envelope and
  updated item response match existing image upload APIs.

The editor stages remote choices until Save. Metadata refresh runs before remote
image selection, preserving the selected artwork without replacing refreshed text.
No database schema changes are required.
