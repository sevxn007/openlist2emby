import { afterEach, describe, expect, it, vi } from "vitest"

import {
  batchRenameFiles,
  buildDownloadUrl,
  cancelTaskRun,
  cancelOpenListTask,
  captureMediaLibraryItemPrimaryImage,
  copyFiles,
  createMediaLibrary,
  grantIdentity,
  createTaskRun,
  createTaskSchedule,
  deleteMediaLibrary,
  deleteTaskRun,
  deleteCompletedTaskRuns,
  deleteTaskSchedule,
  embedMusic,
  getMediaDetail,
  getMediaLibraryItem,
  getMediaLibraryScanSchedule,
  getMediaLibraryStreamURL,
  getMusicDetail,
  listAllMediaLibraries,
  listFiles,
  listMediaLibraries,
  listMediaLibraryItems,
  listManagerUsers,
  listTaskRunItems,
  listTaskRuns,
  listOpenListTasks,
  listTaskSchedules,
  listTaskTypes,
  MANAGER_SESSION_EXPIRED_EVENT,
  ManagerApiError,
  moveFiles,
  removeFiles,
  renameFile,
  retryTaskRun,
  refreshMediaLibraryItemMetadata,
  reorderMediaLibraries,
  runTaskSchedule,
  scanMediaLibrary,
  searchMedia,
  searchMusic,
  setupManager,
  testTMDBConnection,
  testSettingsConnection,
  updateMediaLibrary,
  updateMediaLibraryScanSchedule,
  updateMediaLibraryItemMetadata,
  updateMediaLibraryPlayback,
  updateManagerUser,
  updateTaskSchedule,
} from "@/shared/api/manager"

const fetchMock = vi.fn()

vi.stubGlobal("fetch", fetchMock)

function mockEnvelope(data: unknown, status = 200, message = "success") {
  return new Response(
    JSON.stringify({
      code: status,
      message,
      data,
    }),
    {
      status,
      headers: {
        "Content-Type": "application/json",
      },
    },
  )
}

function mockMusicEmbedStream() {
  return new Response(
    [
      'event: progress\ndata: {"stage":"downloading","percent":42}\n\n',
      'event: progress\ndata: {"stage":"completed","percent":100}\n\n',
      'event: complete\ndata: {"embedded_cover":true,"embedded_lyrics":true}\n\n',
    ].join(""),
    {
      status: 200,
      headers: { "Content-Type": "text/event-stream" },
    },
  )
}

afterEach(() => {
  fetchMock.mockReset()
})

describe("Manager API client", () => {
  it("clears all completed task runs with one unfiltered request", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ deleted: 25 }))

    await expect(deleteCompletedTaskRuns()).resolves.toEqual({ deleted: 25 })
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/tasks/runs/completed",
      expect.objectContaining({ method: "DELETE", credentials: "include" }),
    )
  })

  it("notifies the app when the Manager session expires", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope(null, 401, "login required"))
    const expired = vi.fn()
    window.addEventListener(MANAGER_SESSION_EXPIRED_EVENT, expired, { once: true })

    await expect(listTaskTypes()).rejects.toMatchObject({ code: 401 })
    expect(expired).toHaveBeenCalledOnce()
  })

  it("loads music library roots as audio songs without a synthetic parent", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ items: [], total: 0 }))

    await listMediaLibraryItems({
      libraryId: 3,
      kinds: ["audio"],
      limit: 16,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-library/items?library_id=3&kinds=audio&limit=16",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("refreshes an indexed movie or series from its TMDB ID", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ id: 11, name: "Dune: Part Two" }))

    await refreshMediaLibraryItemMetadata(11, 693134)

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-library/items/11/metadata/refresh",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ tmdb_id: 693134 }),
      }),
    )
  })

  it("refreshes a series with a fixed TVDB source", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ id: 18 }))

    await refreshMediaLibraryItemMetadata(18, { metadata_source: "tvdb", tvdb_id: 72860 })

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-library/items/18/metadata/refresh",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ metadata_source: "tvdb", tvdb_id: 72860 }) }),
    )
  })

  it("refreshes a bound episode without an explicit TMDB ID", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ id: 17 }))

    await refreshMediaLibraryItemMetadata(17)

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-library/items/17/metadata/refresh",
      expect.objectContaining({ method: "POST", body: "{}" }),
    )
  })

  it("captures a primary image from the item's video", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ id: 11, image_url: "/captured.jpg" }))

    await captureMediaLibraryItemPrimaryImage(11)

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-library/items/11/images/primary/capture",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    )
  })

  it("configures OpenList and creates the local manager account", async () => {
    fetchMock.mockResolvedValueOnce(
      mockEnvelope({
        setup_required: false,
      }),
    )

    await setupManager({
      openlist_base_url: "http://openlist.example",
      openlist_admin_key: "admin-key",
      username: "manager",
      password: "password-123",
    })
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/setup",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          openlist_base_url: "http://openlist.example",
          openlist_admin_key: "admin-key",
          username: "manager",
          password: "password-123",
        }),
      }),
    )
  })

  it("manages OpenList authorizations through the administrator endpoints", async () => {
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({ items: [], total: 0 }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          id: 2,
          username: "viewer",
          role: "media",
          enabled: true,
          library_grants: [{ library_id: 7, access: "view" }],
          created_at: "2026-08-01T00:00:00Z",
          updated_at: "2026-08-01T00:00:00Z",
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          id: 2,
          username: "viewer",
          role: "media",
          enabled: false,
          library_grants: [
            { library_id: 7, access: "edit" },
            { library_id: 8, access: "view" },
          ],
          created_at: "2026-08-01T00:00:00Z",
          updated_at: "2026-08-01T00:00:00Z",
        }),
      )
      .mockResolvedValueOnce(mockEnvelope({ deleted: true }))

    await listManagerUsers()
    await grantIdentity({
      openlist_user_id: 12,
      role: "media",
      enabled: true,
      library_grants: [{ library_id: 7, access: "view" }],
    })
    await updateManagerUser(2, {
      enabled: false,
      library_grants: [
        { library_id: 7, access: "edit" },
        { library_id: 8, access: "view" },
      ],
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/users",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/user-authorizations",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          openlist_user_id: 12,
          role: "media",
          enabled: true,
          library_grants: [{ library_id: 7, access: "view" }],
        }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/users/2",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          enabled: false,
          library_grants: [
            { library_id: 7, access: "edit" },
            { library_id: 8, access: "view" },
          ],
        }),
      }),
    )
  })

  it("loads file lists through the manager proxy", async () => {
    fetchMock.mockResolvedValueOnce(
      mockEnvelope({
        content: [],
        total: 0,
        readme: "",
        header: "",
        write: false,
        write_content_bypass: false,
        provider: "unknown",
      }),
    )

    const data = await listFiles({ path: "/Movies" })

    expect(data.total).toBe(0)
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/openlist/fs/list",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          path: "/Movies",
          page: 1,
          per_page: 0,
          refresh: false,
        }),
      }),
    )
  })

  it("throws ManagerApiError when the envelope code is outside 2xx", async () => {
    fetchMock.mockResolvedValueOnce(
      mockEnvelope(null, 403, "permission denied"),
    )

    await expect(listFiles({ path: "/" })).rejects.toMatchObject({
      code: 403,
      message: "permission denied",
    } satisfies Partial<ManagerApiError>)
  })

  it("does not send OpenList tokens from the browser", async () => {
    fetchMock.mockResolvedValueOnce(
      mockEnvelope({
        content: [],
        total: 0,
        readme: "",
        header: "",
        write: false,
        write_content_bypass: false,
        provider: "unknown",
      }),
    )

    await listFiles({ path: "/" })

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/openlist/fs/list",
      expect.objectContaining({
        headers: expect.not.objectContaining({
          Authorization: expect.any(String),
        }),
      }),
    )
  })

  it("tests the current settings without saving them", async () => {
    fetchMock.mockResolvedValueOnce(
      mockEnvelope({
        openlist_base_url: "http://openlist.example",
        username: "admin",
      }),
    )

    await testSettingsConnection({
      openlist_base_url: "http://openlist.example",
      openlist_admin_key: "new-key",
    })

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/settings/test",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          openlist_base_url: "http://openlist.example",
          openlist_admin_key: "new-key",
        }),
      }),
    )
  })

  it("uses manager media endpoints for TMDB test, search, and detail", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ connected: true }))
      .mockResolvedValueOnce(mockEnvelope({ results: [] }))
      .mockResolvedValueOnce(
        mockEnvelope({
          id: 95396,
          media_type: "tv",
          season_number: 1,
          episodes: [],
        }),
      )

    await testTMDBConnection({ tmdb_api_key: "temporary-key" })
    await searchMedia("Severance", "en-US")
    await getMediaDetail("tv", 95396, "zh-CN", 1)

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/media/test",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ tmdb_api_key: "temporary-key" }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/media/search?query=Severance&language=en-US",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/media/detail?type=tv&id=95396&language=zh-CN&season=1",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("uses manager music endpoints for search, detail, and embedding", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ results: [] }))
      .mockResolvedValueOnce(
        mockEnvelope({
          cover_url: "https://images.example/cover.jpg",
          lyrics: "[00:01.00]Lyrics",
        }),
      )
      .mockResolvedValueOnce(mockMusicEmbedStream())

    await searchMusic("Ocean Eyes")
    await getMusicDetail("netease", "picture-1", "lyric-1")
    const onProgress = vi.fn()
    await embedMusic(
      {
        item_id: 17,
        source: "netease",
        picture_id: "picture-1",
        lyric_id: "lyric-1",
        embed_cover: true,
        embed_lyrics: true,
      },
      onProgress,
    )

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/music/search?query=Ocean+Eyes&source=netease",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/music/detail?source=netease&picture_id=picture-1&lyric_id=lyric-1",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/music/embed",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        headers: {
          Accept: "text/event-stream",
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          item_id: 17,
          source: "netease",
          picture_id: "picture-1",
          lyric_id: "lyric-1",
          embed_cover: true,
          embed_lyrics: true,
        }),
      }),
    )
    expect(onProgress).toHaveBeenNthCalledWith(1, {
      stage: "downloading",
      percent: 42,
    })
    expect(onProgress).toHaveBeenNthCalledWith(2, {
      stage: "completed",
      percent: 100,
    })
  })

  it("uses manager media-library endpoints", async () => {
    const input = {
      name: "Movies",
      content_type: "movies" as const,
      root_paths: ["/Media/Movies", "/Media/Archive"],
      video_screenshot_enabled: true,
      video_screenshot_percent: 25,
      playback_completion_percent: 95,
      hide_people_without_images: false,
    }
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ items: [], total: 0 }))
      .mockResolvedValueOnce(mockEnvelope({ id: 7, ...input }, 201))
      .mockResolvedValueOnce(mockEnvelope({ id: 7, ...input }))
      .mockResolvedValueOnce(mockEnvelope({ deleted: true }))
      .mockResolvedValueOnce(mockEnvelope({ id: 91 }, 202))
      .mockResolvedValueOnce(mockEnvelope({ reordered: true }))
      .mockResolvedValueOnce(mockEnvelope({ items: [], total: 0 }))
      .mockResolvedValueOnce(mockEnvelope({ id: 11, library_id: 7 }))
      .mockResolvedValueOnce(mockEnvelope({ id: 11, library_id: 7, name: "Dune" }))
      .mockResolvedValueOnce(
        mockEnvelope({
          playback_position_ticks: 42_000_000,
          played: false,
          play_count: 0,
          favorite: false,
        }),
      )

    await listMediaLibraries()
    await createMediaLibrary(input)
    await updateMediaLibrary(7, input)
    await deleteMediaLibrary(7)
    await scanMediaLibrary(7)
    await reorderMediaLibraries([9, 7])
    await listMediaLibraryItems({
      libraryId: 7,
      parentId: "root",
      kinds: ["movie", "episode"],
      search: "Dune",
      recursive: true,
      latest: true,
      resume: true,
      start: 20,
      limit: 10,
    })
    await getMediaLibraryItem(11)
    await updateMediaLibraryItemMetadata(11, {
      name: "Dune",
      sort_name: "Dune",
      tmdb_id: 438631,
      imdb_id: "tt1160419",
      genres: ["Science Fiction"],
      season_number: null,
      episode_number: null,
      production_year: 2021,
      premiere_date: "2021-10-22",
      overview: "A custom overview",
      community_rating: 8.2,
      runtime_ticks: 9_300_000_000,
      image_url: "https://image.example/dune.jpg",
      backdrop_url: "https://image.example/dune-backdrop.jpg",
      album: "",
      album_artist: "",
      artists: [],
      track_number: null,
      disc_number: null,
      lyrics: "",
      translated_lyrics: "",
    })
    await updateMediaLibraryPlayback(
      11,
      { position_ticks: 42_000_000, stopped: false },
      true,
    )

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/media-libraries",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/media-libraries",
      expect.objectContaining({ method: "POST", body: JSON.stringify(input) }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/media-libraries/7",
      expect.objectContaining({ method: "PUT", body: JSON.stringify(input) }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/manager/media-libraries/7",
      expect.objectContaining({ method: "DELETE" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/manager/media-libraries/7/scan",
      expect.objectContaining({ method: "POST", body: JSON.stringify({}) }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      6,
      "/api/manager/media-libraries/order",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ library_ids: [9, 7] }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      7,
      "/api/manager/media-library/items?library_id=7&parent_id=root&kinds=movie%2Cepisode&search=Dune&recursive=true&latest=true&resume=true&start=20&limit=10",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      8,
      "/api/manager/media-library/items/11",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(getMediaLibraryStreamURL(11)).toBe(
      "/api/manager/media-library/items/11/stream",
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      9,
      "/api/manager/media-library/items/11/metadata",
      expect.objectContaining({
        method: "PUT",
        body: expect.stringContaining('"name":"Dune"'),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      10,
      "/api/manager/media-library/items/11/playback",
      expect.objectContaining({
        method: "PUT",
        keepalive: true,
        body: JSON.stringify({ position_ticks: 42_000_000, stopped: false }),
      }),
    )
  })

  it("requests hidden media libraries for administrator configuration", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ items: [], total: 0 }))

    await listAllMediaLibraries()

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-libraries?include_hidden=true",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("uses media-library scan schedule endpoints", async () => {
    const schedule = {
      id: 12,
      type: "media.scan",
      cron_expression: "0 * * * *",
      timezone: "UTC",
      enabled: true,
    }
    fetchMock
      .mockResolvedValueOnce(mockEnvelope(schedule))
      .mockResolvedValueOnce(mockEnvelope({ ...schedule, enabled: false }))

    await getMediaLibraryScanSchedule(7)
    await updateMediaLibraryScanSchedule(7, {
      cron_expression: "0 * * * *",
      timezone: "UTC",
      enabled: false,
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/media-libraries/7/scan-schedule",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/media-libraries/7/scan-schedule",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          cron_expression: "0 * * * *",
          timezone: "UTC",
          enabled: false,
        }),
      }),
    )
  })

  it("uses OpenList task list and cancel endpoints", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ items: [] }))
      .mockResolvedValueOnce(mockEnvelope({ canceled: true }))

    await listOpenListTasks()
    await cancelOpenListTask({ kind: "copy", id: "copy 1" })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/openlist/tasks",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/openlist/tasks/copy/copy%201/cancel",
      expect.objectContaining({ method: "POST" }),
    )
  })

  it("uses durable task run endpoints and accepts 202 envelopes", async () => {
    const queuedRun = {
      id: 41,
      type: "music.embed_metadata",
      payload_version: 1,
      subject: { path: "/Music/Ocean Eyes.flac" },
      trigger: "manual",
      status: "queued",
      progress: { stage: "preparing", percent: 0 },
      attempt: 0,
      max_attempts: 3,
      available_at: "2026-07-22T00:00:00Z",
      cancel_requested: false,
      created_by: "admin",
      created_at: "2026-07-22T00:00:00Z",
      updated_at: "2026-07-22T00:00:00Z",
    }
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          items: [
            {
              type: "music.embed_metadata",
              payload_version: 1,
              manual: true,
              schedulable: false,
              cancelable: true,
            },
          ],
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({ items: [queuedRun], total: 1, page: 2, per_page: 20 }),
      )
      .mockResolvedValueOnce(mockEnvelope(queuedRun, 202))
      .mockResolvedValueOnce(mockEnvelope({ ...queuedRun, status: "canceled" }))
      .mockResolvedValueOnce(
        mockEnvelope({ ...queuedRun, id: 42, trigger: "retry" }, 202),
      )
      .mockResolvedValueOnce(mockEnvelope({ deleted: true }))

    await listTaskTypes()
    await listTaskRuns({
      statuses: ["queued", "running"],
      type: "music.embed_metadata",
      page: 2,
      perPage: 20,
    })
    await createTaskRun({
      type: "music.embed_metadata",
      payload: {
        path: "/Music/Ocean Eyes.flac",
        source: "netease",
        picture_id: "picture-1",
        lyric_id: "lyric-1",
        embed_cover: true,
        embed_lyrics: true,
      },
    })
    await cancelTaskRun(41)
    await retryTaskRun(41)
    await deleteTaskRun(41)

    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/tasks/runs?status=queued%2Crunning&type=music.embed_metadata&page=2&per_page=20",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/tasks/runs",
      expect.objectContaining({
        method: "POST",
        body: expect.stringContaining('"type":"music.embed_metadata"'),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/manager/tasks/runs/41/cancel",
      expect.objectContaining({ method: "POST" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/manager/tasks/runs/41/retry",
      expect.objectContaining({ method: "POST" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      6,
      "/api/manager/tasks/runs/41",
      expect.objectContaining({ method: "DELETE" }),
    )
  })

  it("uses task schedule CRUD and run-now endpoints", async () => {
    const payload = {
      name: "Library refresh",
      type: "library.refresh",
      cron_expression: "0 3 * * *",
      timezone: "Asia/Shanghai",
      enabled: true,
      payload: { path: "/" },
    }
    const schedule = {
      id: 7,
      ...payload,
      payload_version: 1,
      parameters: payload.payload,
      created_by: "admin",
      created_at: "2026-07-22T00:00:00Z",
      updated_at: "2026-07-22T00:00:00Z",
    }
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ items: [schedule], total: 1 }))
      .mockResolvedValueOnce(mockEnvelope(schedule, 201))
      .mockResolvedValueOnce(mockEnvelope(schedule))
      .mockResolvedValueOnce(mockEnvelope({ deleted: true }))
      .mockResolvedValueOnce(
        mockEnvelope(
          {
            id: 8,
            type: schedule.type,
            payload_version: 1,
            trigger: "run_now",
            status: "queued",
            progress: { stage: "", percent: 0 },
            attempt: 0,
            max_attempts: 3,
            available_at: "2026-07-22T00:00:00Z",
            cancel_requested: false,
            created_by: "admin",
            created_at: "2026-07-22T00:00:00Z",
            updated_at: "2026-07-22T00:00:00Z",
          },
          202,
        ),
      )

    await listTaskSchedules()
    await createTaskSchedule(payload)
    await updateTaskSchedule(7, payload)
    await deleteTaskSchedule(7)
    await runTaskSchedule(7)

    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/tasks/schedules",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify(payload),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/tasks/schedules/7",
      expect.objectContaining({ method: "PUT", body: JSON.stringify(payload) }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/manager/tasks/schedules/7",
      expect.objectContaining({ method: "DELETE" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/manager/tasks/schedules/7/run",
      expect.objectContaining({ method: "POST" }),
    )
  })

  it("uses filtered schedules and generic run item endpoints", async () => {
    const page = {
      items: [],
      total: 0,
      page: 3,
      per_page: 20,
      summary: { statuses: {}, actions: {} },
    }
    fetchMock
      .mockResolvedValueOnce(mockEnvelope(page))
      .mockResolvedValueOnce(mockEnvelope({ items: [], total: 0 }))

    await expect(listTaskRunItems(41, {
      status: "running",
      action: "copy",
      page: 3,
      perPage: 20,
    })).resolves.toEqual(page)
    await listTaskSchedules("media.scan")

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/tasks/runs/41/items?status=running&action=copy&page=3&per_page=20",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/tasks/schedules?type=media.scan",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("builds signed download links using the configured OpenList address", () => {
    expect(
      buildDownloadUrl(
        "/Movies",
        {
          name: "clip one.mp4",
          size: 100,
          is_dir: false,
          modified: "",
          created: "",
          sign: "abc+123",
          thumb: "",
          type: 0,
          hashinfo: "",
        },
        "http://openlist.local/",
      ),
    ).toBe("http://openlist.local/d/Movies/clip%20one.mp4?sign=abc%2B123")
  })

  it("proxies rename, move, and copy operations through the manager", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope(null))
      .mockResolvedValueOnce(mockEnvelope(null))
      .mockResolvedValueOnce(mockEnvelope(null))

    await renameFile({ path: "/old.txt", name: "new.txt" })
    await moveFiles({ src_dir: "/", dst_dir: "/Archive", names: ["new.txt"] })
    await copyFiles({ src_dir: "/", dst_dir: "/Copies", names: ["new.txt"] })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/openlist/fs/rename",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/openlist/fs/move",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          overwrite: false,
          skip_existing: false,
          merge: false,
          src_dir: "/",
          dst_dir: "/Archive",
          names: ["new.txt"],
        }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/openlist/fs/copy",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          overwrite: false,
          skip_existing: false,
          merge: false,
          src_dir: "/",
          dst_dir: "/Copies",
          names: ["new.txt"],
        }),
      }),
    )
  })

  it("proxies remove and batch rename payloads through the manager", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope(null))
      .mockResolvedValueOnce(mockEnvelope(null))

    await removeFiles({ dir: "/Movies", names: ["One.mkv", "Two.mkv"] })
    await batchRenameFiles({
      src_dir: "/Movies",
      rename_objects: [
        { src_name: "One.mkv", new_name: "Episode-01.mkv" },
        { src_name: "Two.mkv", new_name: "Episode-02.mkv" },
      ],
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/manager/openlist/fs/remove",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          dir: "/Movies",
          names: ["One.mkv", "Two.mkv"],
        }),
      }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/openlist/fs/batch_rename",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          src_dir: "/Movies",
          rename_objects: [
            { src_name: "One.mkv", new_name: "Episode-01.mkv" },
            { src_name: "Two.mkv", new_name: "Episode-02.mkv" },
          ],
        }),
      }),
    )
  })
})
