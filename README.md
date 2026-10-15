# openlist2emby

[English](README.md) | [简体中文](README.zh-CN.md)

openlist2emby turns folders in one OpenList instance into movie, TV show, and
music libraries for Emby-compatible clients. It includes a web interface for
library browsing, playback, metadata editing, file management, user access,
and background tasks. The service uses Go, React, and SQLite.

Playback resolves the current OpenList download URL and returns an HTTP 302
redirect. Media streams go directly to the download source; the service does
not proxy or transcode them.

## Features

- Index multiple OpenList folders as movie, TV show, or music libraries.
- Search and refresh movie and TV metadata with configurable TMDB and TVDB sources.
- Cache artwork, credits, lyrics, and metadata locally for browsing.
- Browse and play libraries in the web interface or an Emby-compatible client.
- Track per-user playback progress, watched state, and favorites.
- Authorize OpenList identities with independent roles and library grants.
- Manage OpenList files, batch renaming, and music cover/lyric embedding.
- Schedule library scans and inspect, cancel, retry, or delete background runs.
- Use FFmpeg screenshots as optional movie and episode covers.

## Quick Start With Docker

From the project directory:

```bash
docker compose up -d --build
```

Open `http://127.0.0.1:8091` and complete setup using:

- Your OpenList base URL and administrator integration key.
- An existing OpenList administrator username and password.
- A verification code if that account uses two-factor authentication.

Setup validates both the integration key and administrator identity. It does
not create an account or store the password. When OpenList runs on the Docker
host, use `http://host.docker.internal:<port>` instead of the container's
loopback address.

Enable the media library in Settings, configure metadata credentials as needed,
and create a library with one or more OpenList root folders. Authorize users
in Users before they can log in. Emby-compatible clients connect to
`http://<server-host>:8096/emby` using an authorized OpenList account.
Emby login does not support accounts with two-factor authentication.

The image is named `openlist2emby:latest` and runs as an unprivileged user.
The `openlist2emby-data` Compose volume stores the SQLite database and media
cache under `/data`. FFmpeg and FFprobe are included.

### Docker Ports

`OPENLIST2EMBY_HTTP_PORT` changes the host port for the web interface.
`OPENLIST2EMBY_EMBY_PORT` publishes the Emby listener port; when changing it,
set the same port in Settings and recreate the service.

```bash
OPENLIST2EMBY_HTTP_PORT=9081 \
OPENLIST2EMBY_EMBY_PORT=8097 \
docker compose up -d
```

Compose accepts `OPENLIST2EMBY_TASK_WORKERS` (default `1`) and
`OPENLIST2EMBY_TASK_RETENTION_DAYS` (default `30`). It builds the image locally;
no remote container registry is required by default.

### Publishing And Updating Docker Images

The GitHub Actions workflow publishes multi-platform images to GitHub Container
Registry (GHCR). Pushing to `main` publishes `ghcr.io/<owner>/openlist2emby:dev`
and a commit-specific `sha-...` tag. Publishing a GitHub Release publishes an
image tagged with the release tag; stable releases also update `latest`.

On the server, set the image in `.env` to the desired tag:

```dotenv
OPENLIST2EMBY_IMAGE=ghcr.io/<owner>/openlist2emby:dev
```

Then pull and recreate the service without building locally:

```bash
docker compose pull openlist2emby
docker compose up -d --no-build openlist2emby
```

For a stable deployment, use an explicit release tag such as
`ghcr.io/<owner>/openlist2emby:v1.2.3` instead of `dev` or `latest`. If the GHCR
package is private, log in on the server with a GitHub token that can read
packages before pulling. The Compose data volume is preserved when recreating
the service; back it up before upgrades and do not run `docker compose down -v`.

## Authentication And Permissions

OpenList is the sole credential authority. openlist2emby binds identities by
instance ID and immutable OpenList user ID. Usernames are display values;
passwords, password hashes, and OpenList user tokens are not persisted.

New users have no access until an administrator grants an independent `admin`
or `media` role and enables access. Media users can view and play only granted
libraries; `edit` grants also permit metadata and artwork maintenance. Directory
operations, settings, tasks, and user authorization remain administrator-only
and use the server-side integration key.

Browser sessions use an HTTP-only, SameSite=Lax `openlist2emby_session` cookie
with a sliding 24-hour lifetime. Emby device sessions last 30 days, renewed on
successful local authentication. Requests check local session expiry, current
access, roles, grants, and instance binding. OpenList is contacted to validate
identity at login, not on subsequent session requests. Upstream account changes
do not automatically revoke established local sessions; revoke local access or
devices when immediate logout is needed.

The OpenList address is fixed after setup. Connecting to another instance
requires a fresh installation. Runtime data contains sensitive integration
credentials and session state; protect database files and backups accordingly.

## Metadata And Playback

Movie and TV metadata settings support TMDB, TVDB, an optional TVDB subscriber
PIN, and source priority. Individual movies and series can select `auto`, `tmdb`,
or `tvdb` and save provider IDs. Search supports a title with a release year,
such as `Title (2006)`. Credentials remain server-side and API responses mask them.

Music scans use NetEase Cloud Music, QQ Music, and Kuwo Music. Matched song
metadata, covers, and lyrics are cached locally. Browsing does not query external
metadata services. Artwork and metadata from external providers remain subject
to their attribution and licensing requirements.

Library scans are durable `media.scan` tasks. New libraries receive an hourly
scan schedule in UTC, configurable in library settings. Tasks also support music
metadata embedding (`music.embed_metadata`) and local metadata relocation
(`media.metadata.relocate`). Closing the browser does not stop submitted work.

To import new episodes without scanning the whole library, select files or
folders in the file browser and choose **Import media**. Choose a target library
containing all selected paths (a unique match is selected automatically). This
refreshes only the selected subtrees or file parents and adds or updates their
media without removing existing items. Progress and retries are available in
Tasks; full scans still reconcile deleted files.

In TV libraries, video files without season/episode numbers are indexed as
individual series with one playable episode. The series title is parsed from the
filename and matched against TV metadata; provider season and episode details
are not assigned to the unnumbered file. Emby clients see the normal
Series -> Season 1 -> Episode hierarchy.

The media library is disabled by default. Disabling it hides the library
interface and returns HTTP 404 from media-library and Emby endpoints.

## Development

Requirements:

- Go 1.26.5 and Node.js 24, with versions recorded in `.tool-versions`.
- pnpm 11.7.0, recorded in `frontend/package.json`.
- A reachable OpenList instance and its administrator integration key.
- FFmpeg and FFprobe on `PATH` for native video screenshots.

Start the backend:

```bash
cd backend
go run ./cmd/server
```

Start the frontend in another terminal:

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

Vite runs on `http://127.0.0.1:5174` and proxies `/api/manager/*` to
`http://127.0.0.1:8091`. Set `VITE_OPENLIST2EMBY_BASE` in `frontend/.env` when
the backend uses another origin; `frontend/.env.example` provides an example.
The Emby listener is separate and uses the port configured in Settings.

### Native Build And Run

From the repository root:

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend build
go build -C backend -o openlist2emby ./cmd/server
./backend/openlist2emby \
  -addr :8091 \
  -db ./backend/data/openlist2emby.db \
  -static ./frontend/dist
```

The Go service serves the web interface and Manager API on the same listener.
Relative paths are resolved from the process working directory.

## Runtime Configuration

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `-addr` | `OPENLIST2EMBY_ADDR` | `127.0.0.1:8091` | HTTP listen address; Docker uses `:8091` |
| `-db` | `OPENLIST2EMBY_DB` | `./data/openlist2emby.db` | SQLite database path; Docker uses `/data/openlist2emby.db` |
| `-static` | — | empty | Built frontend directory |
| `-task-workers` | `OPENLIST2EMBY_TASK_WORKERS` | `2` | Worker count, from 1 to 32 |
| `-task-retention-days` | `OPENLIST2EMBY_TASK_RETENTION_DAYS` | `30` | Completed task retention in days |

Flags override environment-backed defaults. Install into an empty database and
metadata directory. The current SQLite schema is authoritative; pre-release
schema compatibility is not guaranteed. Startup never clears existing data.

## Repository And Verification

```text
openlist2emby/
├── backend/   # Go service, integrations, SQLite, and task engine
└── frontend/  # React, Vite, TypeScript, Tailwind CSS, and Radix UI
```

See [backend documentation](backend/README.md) ([简体中文](backend/README.zh-CN.md)),
[frontend documentation](frontend/README.md) ([简体中文](frontend/README.zh-CN.md)),
[user authorization APIs](USER_MANAGEMENT_API.md), and
[Emby compatibility](EMBY_API_COMPATIBILITY.md). Contributor guidelines are in
[AGENTS.md](AGENTS.md) ([简体中文](AGENTS.zh-CN.md)).

```bash
cd backend
go test ./...
go vet ./...
cd ../frontend
pnpm lint
pnpm test
pnpm build
```

## License

This project is licensed under the [MIT License](LICENSE).
Third-party dependencies and assets retain their respective licenses;
license and attribution review is required before an open-source release.
