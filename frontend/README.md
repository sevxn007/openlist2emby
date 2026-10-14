# openlist2emby Frontend

[English](README.md) | [简体中文](README.zh-CN.md)

The React interface provides first-run setup, OpenList identity login, library
browsing and playback, metadata editing, file management, user authorization,
settings, and background tasks. API access is centralized in `src/shared/api`;
server state uses TanStack Query. Visible product text is available in English
and Simplified Chinese through `src/shared/i18n`.

## Development

Use Node.js 24 and pnpm 11.7.0:

```bash
pnpm install --frozen-lockfile
cp .env.example .env
pnpm dev
```

`VITE_OPENLIST2EMBY_BASE` sets the development backend origin and defaults to
`http://127.0.0.1:8091`. Vite serves the frontend on port `5174` and proxies
`/api/manager/*`. Production builds use same-origin APIs served by the Go service.
Emby clients use the separate listener configured in Settings.

```bash
pnpm lint
pnpm test
pnpm build
pnpm preview
```

## Authentication And User Access

Setup validates an existing OpenList administrator and integration key. Login
accepts an OpenList username, password, and optional verification code. The
browser receives the HTTP-only `openlist2emby_session` cookie; passwords,
integration credentials, and OpenList user tokens are never stored in the browser.

Administrators select OpenList identities and grant independent roles, enabled
access, and library `view`/`edit` permissions. Account creation and credential
changes belong in OpenList. Media users see only authorized libraries.

Theme, language, sidebar state, audio-player preferences, and media-search history use browser storage
keys under `openlist2emby.`. The initial theme script and React theme provider
read the same `openlist2emby.theme` preference.

## Media Metadata And Tasks

Settings configure TMDB and TVDB credentials, an optional TVDB subscriber PIN,
and source priority. Movies and series can store `tmdb_id`, `tvdb_id`, and a
`metadata_source` of `auto`, `tmdb`, or `tvdb`. Explicit refresh requests use these
fields and require library edit access. Provider failures retain stored metadata.

Artwork uses authenticated local image endpoints. Administrators can relocate
the metadata directory through a durable task. Music scans use NetEase Music,
QQ Music, and Kuwo Music and cache matched metadata, lyrics, and covers locally.

Library settings own scan schedules. The task center shows progress and run
history and supports cancellation, deletion of non-running runs, and retry of
failed or canceled registered tasks. Music embedding submits a
`music.embed_metadata` task from an audio item's detail page; submitted work
continues after the browser closes.
