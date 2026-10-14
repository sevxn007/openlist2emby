import {fireEvent, screen, waitFor, within} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest"

import {toast} from "sonner"

import {MediaLibraryPage} from "@/features/media/MediaLibraryPage"
import {renderWithProviders} from "@/test/render"

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: status, message: "success", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const library = {
  id: 7,
  name: "Movies",
  content_type: "movies",
  root_path: "/Media/Movies",
  root_paths: ["/Media/Movies"],
  video_screenshot_enabled: false,
  video_screenshot_percent: 10,
  playback_completion_percent: 90,
  hide_people_without_images: false,
  last_scanned_at: "2026-07-27T08:00:00Z",
  created_at: "2026-07-27T07:00:00Z",
  updated_at: "2026-07-27T08:00:00Z",
}

const movie = {
  id: 11,
  library_id: 7,
  kind: "movie",
  path: "/Media/Movies/Dune.mkv",
  media_path: "/Media/Movies/Dune.mkv",
  name: "Dune",
  sort_name: "Dune",
  container: "mkv",
  size_bytes: 1024,
  production_year: 2021,
  image_url: "https://image.example/dune.jpg",
  user_data: {
    playback_position_ticks: 0,
    played: false,
    play_count: 0,
    favorite: false,
  },
  created_at: "2026-07-27T08:00:00Z",
  updated_at: "2026-07-27T08:00:00Z",
}

const tvLibrary = {
  ...library,
  name: "TV Shows",
  content_type: "tvshows",
  root_path: "/Media/TV",
  root_paths: ["/Media/TV"],
}

const series = {
  ...movie,
  id: 21,
  kind: "series",
  path: "/Media/TV/Severance",
  media_path: undefined,
  name: "Severance",
  sort_name: "Severance",
  container: undefined,
  production_year: 2022,
  image_url: "https://image.example/severance-poster.jpg",
  backdrop_url: "https://image.example/severance-backdrop.jpg",
}

const season = {
  ...series,
  id: 22,
  parent_id: 21,
  kind: "season",
  path: "/Media/TV/Severance/Season 1",
  name: "Season 1",
  sort_name: "Season 1",
  season_number: 1,
  image_url: "https://image.example/severance-season-1.jpg",
}

const episode = {
  ...movie,
  id: 23,
  parent_id: 22,
  kind: "episode",
  path: "/Media/TV/Severance/Season 1/S01E01.mkv",
  media_path: "/Media/TV/Severance/Season 1/S01E01.mkv",
  name: "Good News About Hell",
  sort_name: "Good News About Hell",
  season_number: 1,
  episode_number: 1,
  production_year: 2022,
  community_rating: 8.4,
  image_url: "https://image.example/severance-s01e01-still.jpg",
}

const musicLibrary = {
  ...library,
  id: 8,
  name: "Music",
  content_type: "music",
  root_path: "/Media/Music",
  root_paths: ["/Media/Music"],
}

const audio = {
  ...movie,
  id: 31,
  library_id: 8,
  kind: "audio",
  path: "/Media/Music/Song.flac",
  media_path: "/Media/Music/Song.flac",
  name: "Song",
  sort_name: "Song",
  container: "flac",
  production_year: undefined,
  image_url: "/api/manager/media-library/items/31/images/primary?tag=cover",
  image_source_url: "https://image.example/song-cover.jpg",
  album: "Original album",
  album_artist: "Original artist",
  artists: ["Original artist"],
  track_number: 1,
  disc_number: 1,
  lyrics: "[00:01.00]Original line",
  translated_lyrics: "",
}

const scanSchedule = {
  id: 17,
  name: "Movies",
  type: "media.scan",
  payload_version: 1,
  parameters: { library_id: 7, library_name: "Movies" },
  cron_expression: "0 * * * *",
  timezone: "UTC",
  enabled: true,
  next_run_at: "2026-07-27T09:00:00Z",
  created_by: "admin",
  created_at: "2026-07-27T08:00:00Z",
  updated_at: "2026-07-27T08:00:00Z",
}

beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
  document.documentElement.classList.remove("dark")
  document.documentElement.style.colorScheme = ""
})

describe("MediaLibraryPage", () => {
  it("loads the next continue-watching page without losing previous entries", async () => {
    const entries = Array.from({ length: 26 }, (_, i) => ({
      item: { ...movie, id: 200 + i, name: `Recent ${i + 1}` },
      source: "resume",
    }))
    const starts: number[] = []
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") return Promise.resolve(envelope({ items: [library], total: 1 }))
      if (url.includes("/continue-watching?")) {
        const start = Number(new URL(url, "http://localhost").searchParams.get("start"))
        starts.push(start)
        return Promise.resolve(envelope({ items: entries.slice(start, start + 24), total: 26 }))
      }
      return Promise.resolve(envelope({ items: [], total: 0 }))
    })
    renderWithProviders(<MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />)
    const region = await screen.findByRole("region", { name: "Continue Watching" })
    const first = within(region).getByRole("button", { name: "Recent 1" })
    const rail = first.closest(".overflow-x-auto")
    if (!(rail instanceof HTMLElement)) throw new Error("Missing recent rail")
    const scrollBy = vi.fn()
    Object.defineProperties(rail, {
      clientWidth: { configurable: true, value: 800 },
      scrollWidth: { configurable: true, value: 800 },
      scrollBy: { configurable: true, value: scrollBy },
    })
    await userEvent.click(within(region).getByRole("button", { name: "Next items in Continue Watching" }))
    expect(await within(region).findByRole("button", { name: "Recent 26" })).toBeInTheDocument()
    expect(first).toBeInTheDocument()
    expect(starts).toEqual([0, 24])
    await waitFor(() => expect(scrollBy).toHaveBeenCalledWith({ left: 640, behavior: "smooth" }))
    expect(within(region).queryByRole("button", { name: "Next items in Continue Watching" })).not.toBeInTheDocument()
  })

  it("loads more home titles from the right arrow when the first eight fit", async () => {
    const items = Array.from({ length: 10 }, (_, i) => ({ ...movie, id: 100 + i, name: `Title ${i + 1}` }))
    let failNext = true
    const starts: number[] = []
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") return Promise.resolve(envelope({ items: [library], total: 1 }))
      if (url.startsWith("/api/manager/media-library/items?")) {
        const params = new URL(url, "http://localhost").searchParams
        const start = Number(params.get("start") ?? 0)
        starts.push(start)
        if (start === 8 && failNext) return Promise.resolve(envelope(null, 500))
        return Promise.resolve(envelope({ items: items.slice(start, start + 8), total: 10 }))
      }
      return Promise.resolve(envelope({ items: [], total: 0 }))
    })
    renderWithProviders(<MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />)
    const first = await screen.findByRole("button", { name: "Title 1" })
    const rail = first.closest(".overflow-x-auto")
    if (!(rail instanceof HTMLElement)) throw new Error("Missing home rail")
    const scrollBy = vi.fn()
    Object.defineProperties(rail, {
      clientWidth: { configurable: true, value: 800 },
      scrollWidth: { configurable: true, value: 800 },
      scrollBy: { configurable: true, value: scrollBy },
    })
    await userEvent.click(screen.getByRole("button", { name: "Next items in Movies" }))
    await waitFor(() => expect(starts).toContain(8))
    await waitFor(() => expect(screen.getByRole("button", { name: "Next items in Movies" })).toBeEnabled())
    expect(screen.getByRole("button", { name: "Title 1" })).toBeInTheDocument()
    failNext = false
    await userEvent.click(screen.getByRole("button", { name: "Next items in Movies" }))
    expect(await screen.findByRole("button", { name: "Title 10" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Title 1" })).toBeInTheDocument()
    expect(starts).toEqual([0, 8, 8])
    await waitFor(() => expect(scrollBy).toHaveBeenCalledWith({ left: 640, behavior: "smooth" }))
    expect(screen.queryByRole("button", { name: "Next items in Movies" })).not.toBeInTheDocument()
  })

  it.each(["light", "dark"] as const)(
    "uses global theme tokens for the %s media home, browse, and detail views",
    async (theme) => {
      window.localStorage.setItem("openlist2emby.theme", theme)
      vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (url === "/api/manager/tasks/schedules?type=media.scan") {
          return Promise.resolve(envelope({ items: [], total: 0 }))
        }
        if (url.startsWith("/api/manager/media-library/continue-watching?")) {
          return Promise.resolve(envelope({ items: [], total: 0 }))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [{ ...movie, backdrop_url: "https://image.example/backdrop.jpg" }], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })

      renderWithProviders(
        <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
      )

      await screen.findByRole("heading", { name: "My Media" })
      await waitFor(() =>
        expect(document.documentElement.classList.contains("dark")).toBe(theme === "dark"),
      )
      const surface = document.querySelector<HTMLElement>(".media-library-surface")
      expect(surface).toHaveClass("bg-background", "text-foreground")
      expect(surface?.className).not.toContain("bg-[#0c0e0f]")

      await userEvent.click(within(screen.getByRole("region", { name: "My Media" })).getByRole("button", { name: "Movies" }))
      const search = await screen.findByPlaceholderText("Search this library")
      expect(search).toHaveClass("border-border", "bg-muted/50", "text-foreground")

      await userEvent.click(await screen.findByRole("button", { name: "Dune" }))
      const detail = document.querySelector<HTMLElement>("[data-media-detail]")
      expect(detail).toHaveClass("bg-background", "text-foreground")
      expect(detail?.className).not.toContain("bg-[#0c0e0f]")
      expect(detail?.querySelector("[data-media-backdrop]")?.getAttribute("style")).toContain("var(--background)")
      expect(screen.getByRole("button", { name: "Play" })).toHaveClass("bg-primary", "text-primary-foreground")
    },
  )

  describe.each(["light", "dark"])("%s loading and empty states", (theme) => {
    it.each([200, 503])("themes the loading skeleton and %i response", async (status) => {
      window.localStorage.setItem("openlist2emby.theme", theme)
      vi.spyOn(window, "fetch").mockImplementation((input) =>
        Promise.resolve(envelope({ items: [], total: 0 }, String(input) === "/api/manager/media-libraries" ? status : 200)),
      )
      const { container } = renderWithProviders(
        <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
      )
      expect(container.querySelector(".media-library-surface")).toHaveClass("bg-background")
      expect(container.querySelector('[data-slot="skeleton"]')).toHaveClass("bg-muted")
      await waitFor(() => expect(container.querySelector('[data-slot="empty"]')).toBeInTheDocument())
      expect(container.querySelector(".media-library-surface")).toHaveClass("bg-background", "text-foreground")
      expect(container.querySelector('[data-slot="empty"]')).toHaveClass("border-border", "text-foreground")
    })
  })

  it("scrolls through media libraries with the shelf controls", async () => {
    const libraries = Array.from({ length: 6 }, (_, index) => ({
      ...library,
      id: index + 1,
      name: `Library ${index + 1}`,
    }))
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") {
        return Promise.resolve(envelope({ items: libraries, total: libraries.length }))
      }
      if (url === "/api/manager/tasks/schedules?type=media.scan") {
        return Promise.resolve(envelope({ items: [], total: 0 }))
      }
      if (url.startsWith("/api/manager/media-library/items?")) {
        return Promise.resolve(envelope({ items: [], total: 0 }))
      }
      return Promise.resolve(envelope(null, 404))
    })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    await screen.findByRole("button", { name: "Library 6" })
    const shelf = document.querySelector<HTMLElement>("[data-library-shelf]")
    expect(shelf).not.toBeNull()
    if (!shelf) return

    let scrollLeft = 0
    Object.defineProperties(shelf, {
      clientWidth: { configurable: true, value: 800 },
      scrollWidth: { configurable: true, value: 1600 },
      scrollLeft: {
        configurable: true,
        get: () => scrollLeft,
        set: (value: number) => { scrollLeft = value },
      },
      scrollBy: { configurable: true, value: vi.fn() },
    })
    fireEvent(window, new Event("resize"))

    const nextButton = await screen.findByRole("button", { name: "Next items in My Media" })
    expect(screen.queryByRole("button", { name: "Previous items in My Media" })).not.toBeInTheDocument()
    expect(nextButton.parentElement).toHaveClass("relative")
    await userEvent.click(nextButton)
    expect(shelf.scrollBy).toHaveBeenCalledWith({ left: 640, behavior: "smooth" })

    shelf.scrollLeft = 800
    fireEvent.scroll(shelf)
    const previousButton = screen.getByRole("button", { name: "Previous items in My Media" })
    expect(screen.queryByRole("button", { name: "Next items in My Media" })).not.toBeInTheDocument()
    await userEvent.click(previousButton)
    expect(shelf.scrollBy).toHaveBeenCalledWith({ left: -640, behavior: "smooth" })
  })

  it("edits localized music metadata from an audio detail", async () => {
    const searchResults = [
      ...Array.from({ length: 6 }, (_, index) => ({
        id: `other-${index + 1}`,
        name: `Other song ${index + 1}`,
        artists: ["Other artist"],
        album: "Other album",
        picture_id: "matched-picture",
        lyric_id: `other-lyric-${index + 1}`,
        source: "netease",
      })),
      {
        id: "matched-song",
        name: "Matched song",
        artists: ["Matched artist"],
        album: "Matched album",
        picture_id: "matched-picture",
        lyric_id: "matched-lyric",
        source: "netease",
      },
    ]
    const updatedAudio = {
      ...audio,
      name: "Matched song",
      sort_name: "Matched song",
      album: "Matched album",
      album_artist: "Matched artist",
      artists: ["Matched artist"],
      image_source_url: "https://image.example/matched-cover.jpg",
      lyrics: "Matched lyrics",
      translated_lyrics: "Matched translation",
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [musicLibrary], total: 1 }))
        }
        if (url === "/api/manager/media-library/items/31/metadata" && init?.method === "PUT") {
          return Promise.resolve(envelope(updatedAudio))
        }
        if (
          url === "/api/manager/media-library/items/31/images/primary" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(envelope({
            ...updatedAudio,
            image_url: "/api/manager/media-library/items/31/images/primary?tag=uploaded",
            image_source_url: "",
          }))
        }
        if (url === "/api/manager/music/search?query=Song+Original+artist&source=netease") {
          return Promise.resolve(envelope({ results: searchResults }))
        }
        if (url === "/api/manager/music/detail?source=netease&picture_id=matched-picture") {
          return Promise.resolve(envelope({
            cover_url: "https://image.example/matched-cover.jpg",
          }))
        }
        if (
          url ===
          "/api/manager/music/detail?source=netease&picture_id=matched-picture&lyric_id=matched-lyric"
        ) {
          return Promise.resolve(envelope({
            cover_url: "https://image.example/matched-cover.jpg",
            lyrics: "Matched lyrics",
            translated_lyrics: "Matched translation",
          }))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [audio], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

	await userEvent.click(await screen.findByRole("button", { name: "Song" }))
	expect(screen.getByRole("img", { name: "Song" })).toHaveAttribute("src", audio.image_url)
	expect(screen.getByText("Artists").nextElementSibling).toHaveTextContent("Original artist")
	expect(screen.getByText("Lyrics")).toBeInTheDocument()
	expect(screen.getByText("Original line")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Embed metadata" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Edit metadata" }))

    expect(screen.queryByLabelText("TMDB ID")).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Search music metadata" }))
    expect(screen.getByLabelText("Song search")).toHaveValue("Song Original artist")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))
    expect(await screen.findByText("1 / 2")).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen
          .getByRole("dialog", { name: "Search music metadata" })
          .querySelector('img[src="https://image.example/matched-cover.jpg"]'),
      ).not.toBeNull()
    })
    await userEvent.click(screen.getByRole("button", { name: "Next page" }))
    await userEvent.click(await screen.findByRole("button", { name: /Matched song/ }))
    await userEvent.click(await screen.findByRole("button", { name: "Use this song" }))

    expect(screen.getByLabelText("Title")).toHaveValue("Matched song")
    expect(screen.getByLabelText("Album")).toHaveValue("Matched album")
    expect(screen.getByLabelText("Original lyrics")).toHaveValue("Matched lyrics")
    const cover = new File(["cover"], "cover.png", { type: "image/png" })
    await userEvent.upload(screen.getByLabelText("Cover image"), cover)
    await userEvent.click(screen.getByRole("button", { name: "Save metadata" }))

    await waitFor(() => {
      const request = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/manager/media-library/items/31/metadata" &&
          init?.method === "PUT",
      )
      expect(JSON.parse(String(request?.[1]?.body))).toEqual(
        expect.objectContaining({
          name: "Matched song",
          sort_name: "Matched song",
          album: "Matched album",
          album_artist: "Matched artist",
          artists: ["Matched artist"],
          image_source_url: "",
          track_number: 1,
          disc_number: 1,
          lyrics: "Matched lyrics",
          translated_lyrics: "Matched translation",
        }),
      )
    })
    const uploadRequest = fetchMock.mock.calls.find(
      ([input, init]) =>
        String(input) === "/api/manager/media-library/items/31/images/primary" &&
        init?.method === "PUT",
    )
    expect(uploadRequest?.[1]?.body).toBeInstanceOf(FormData)
    expect((uploadRequest?.[1]?.body as FormData).get("file")).toEqual(cover)
    expect(uploadRequest?.[1]?.headers).toBeUndefined()
  })

  it("browses indexed media and queues a scan", async () => {
    const onTaskCreated = vi.fn()
    const run = {
      id: 91,
      type: "media.scan",
      payload_version: 1,
      subject: { library_id: 7 },
      trigger: "manual",
      status: "succeeded",
      progress: { stage: "completed", percent: 100 },
      attempt: 1,
      max_attempts: 3,
      available_at: "2026-07-27T08:00:00Z",
      cancel_requested: false,
      created_by: "admin",
      created_at: "2026-07-27T08:00:00Z",
      updated_at: "2026-07-27T08:00:00Z",
      result: { added: 1, updated: 0, removed: 0 },
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (url === "/api/manager/tasks/schedules?type=media.scan") {
          return Promise.resolve(envelope({ items: [scanSchedule], total: 1 }))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [movie], total: 1 }))
        }
        if (
          url === "/api/manager/media-libraries/7/scan" &&
          init?.method === "POST"
        ) {
          return Promise.resolve(envelope(run, 202))
        }
        if (url === "/api/manager/tasks/runs/91") {
          return Promise.resolve(envelope(run))
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage
        onTaskCreated={onTaskCreated}
        onViewTask={vi.fn()}
        onPlay={vi.fn()}
      />,
    )

    expect(await screen.findByRole("button", { name: /Dune/ })).toBeInTheDocument()
    expect(await screen.findByText(/Next scan/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Scan library" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Library actions" })).not.toBeInTheDocument()
    await userEvent.click(screen.getAllByRole("button", { name: "Movies" })[0])
    expect(await screen.findByRole("button", { name: "Library actions" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Scan library" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-libraries/7/scan",
        expect.objectContaining({ method: "POST" }),
      ),
    )
    expect(onTaskCreated).toHaveBeenCalledWith(expect.objectContaining({ id: 91 }))
    await userEvent.click(screen.getByRole("button", { name: "Media home" }))
    expect(screen.queryByRole("button", { name: "Library actions" })).not.toBeInTheDocument()
  })

  it("opens the add-library dialog with an OpenList folder tree", async () => {
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") {
        return Promise.resolve(envelope({ items: [], total: 0 }))
      }
      if (url === "/api/manager/openlist/fs/list") {
        return Promise.resolve(
          envelope({
            content: [
              {
                name: "Media",
                size: 0,
                is_dir: true,
                modified: "",
                created: "",
                sign: "",
                thumb: "",
                type: 1,
                hashinfo: "",
              },
            ],
            total: 1,
          }),
        )
      }
      return Promise.resolve(envelope(null, 404))
    })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )
    expect(
      await screen.findAllByRole("button", { name: "Add library" }),
    ).toHaveLength(1)
    await userEvent.click(
      screen.getByRole("button", { name: "Add library" }),
    )

    const dialog = await screen.findByRole("dialog", { name: "Add library" })
    expect(dialog).toBeInTheDocument()
    expect(dialog).toHaveClass(
      "grid-rows-[auto_minmax(0,1fr)_auto]",
      "overflow-hidden",
    )
    expect(dialog.querySelector('[data-slot="dialog-body"]')).toHaveClass(
      "overflow-y-auto",
    )
    expect(screen.getByRole("switch", { name: "Scheduled scanning" })).toBeChecked()
    expect(screen.getByLabelText("Cron expression")).toHaveValue("0 * * * *")
    expect(screen.getByLabelText("Timezone")).toHaveValue("UTC")
    await userEvent.click(screen.getByRole("button", { name: "Select OpenList folders" }))
    expect(await screen.findByRole("button", { name: "Select folder Root" })).toBeInTheDocument()
  })

  it("loads and updates the selected library scan schedule", async () => {
    const customSchedule = {
      ...scanSchedule,
      cron_expression: "30 2 * * *",
      timezone: "Asia/Shanghai",
      enabled: false,
      next_run_at: undefined,
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (url === "/api/manager/tasks/schedules?type=media.scan") {
          return Promise.resolve(envelope({ items: [customSchedule], total: 1 }))
        }
        if (url === "/api/manager/media-libraries/7/scan-schedule") {
          if (init?.method === "PUT") {
            return Promise.resolve(envelope({ ...customSchedule, enabled: true }))
          }
          return Promise.resolve(envelope(customSchedule))
        }
        if (
          url === "/api/manager/media-libraries/7" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(envelope(library))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [], total: 0 }))
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )
    await userEvent.click((await screen.findAllByRole("button", { name: "Movies" }))[0])
    await userEvent.click(await screen.findByRole("button", { name: "Library actions" }))
    await userEvent.click(await screen.findByRole("menuitem", { name: "Edit library" }))

    expect(await screen.findByLabelText("Cron expression")).toHaveValue("30 2 * * *")
    expect(screen.getByLabelText("Timezone")).toHaveValue("Asia/Shanghai")
    expect(screen.getByRole("switch", { name: "Scheduled scanning" })).not.toBeChecked()
    await userEvent.click(screen.getByRole("switch", { name: "Scheduled scanning" }))
    await userEvent.click(screen.getByRole("button", { name: "Save library" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-libraries/7/scan-schedule",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            cron_expression: "30 2 * * *",
            timezone: "Asia/Shanghai",
            enabled: true,
          }),
        }),
      ),
    )
  })

  it("saves multiple selected OpenList folders in one media library", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries" && init?.method === "POST") {
          return Promise.resolve(
            envelope(
              {
                ...library,
                id: 8,
                name: "All Movies",
                root_path: "/Archive",
                root_paths: ["/Archive", "/Movies"],
              },
              201,
            ),
          )
        }
        if (
          url === "/api/manager/media-libraries/8/scan-schedule" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(envelope({ ...scanSchedule, name: "All Movies" }))
        }
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [], total: 0 }))
        }
        if (url === "/api/manager/openlist/fs/list") {
          return Promise.resolve(
            envelope({
              content: [
                {
                  name: "Movies",
                  size: 0,
                  is_dir: true,
                  modified: "",
                  created: "",
                  sign: "",
                  thumb: "",
                  type: 1,
                  hashinfo: "",
                },
                {
                  name: "Archive",
                  size: 0,
                  is_dir: true,
                  modified: "",
                  created: "",
                  sign: "",
                  thumb: "",
                  type: 1,
                  hashinfo: "",
                },
              ],
              total: 2,
            }),
          )
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )
    await userEvent.click(await screen.findByRole("button", { name: "Add library" }))
    await userEvent.type(screen.getByLabelText("Library name"), "All Movies")
    await userEvent.click(
      screen.getByRole("switch", { name: "Hide cast & crew without profile images" }),
    )
    await userEvent.click(screen.getByRole("switch", { name: "Video screenshot covers" }))
    await userEvent.clear(screen.getByLabelText("Capture position"))
    await userEvent.type(screen.getByLabelText("Capture position"), "25")
    await userEvent.clear(screen.getByLabelText("Playback completion threshold"))
    await userEvent.type(screen.getByLabelText("Playback completion threshold"), "95")
    await userEvent.click(screen.getByRole("button", { name: "Select OpenList folders" }))
    await userEvent.click(await screen.findByRole("button", { name: "Select folder Archive" }))
    await userEvent.click(screen.getByRole("button", { name: "Select folder Movies" }))

    expect(screen.getByRole("button", { name: "Select folder Archive" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    expect(screen.getByRole("button", { name: "Select folder Movies" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    await userEvent.click(screen.getByRole("button", { name: "Select folder Archive" }))
    expect(screen.getByRole("button", { name: "Select folder Archive" })).toHaveAttribute(
      "aria-pressed",
      "false",
    )
    await userEvent.click(screen.getByRole("button", { name: "Select folder Archive" }))
    await userEvent.click(screen.getByRole("button", { name: "Remove folder /Movies" }))
    await userEvent.click(screen.getByRole("button", { name: "Select folder Movies" }))
    await userEvent.click(screen.getByRole("button", { name: "Use selected folders" }))
    await userEvent.click(screen.getByRole("button", { name: "Save library" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-libraries",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            name: "All Movies",
            content_type: "movies",
            root_paths: ["/Archive", "/Movies"],
            video_screenshot_enabled: true,
            video_screenshot_percent: 25,
            playback_completion_percent: 95,
            hide_people_without_images: true,
          }),
        }),
      ),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media-libraries/8/scan-schedule",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          cron_expression: "0 * * * *",
          timezone: "UTC",
          enabled: true,
        }),
      }),
    )
  })

  it("uses a valid screenshot default when switching to a music library", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries" && init?.method === "POST") {
          return Promise.resolve(envelope({ ...musicLibrary, name: "Music" }, 201))
        }
        if (
          url === "/api/manager/media-libraries/8/scan-schedule" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(envelope({ ...scanSchedule, name: "Music" }))
        }
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [], total: 0 }))
        }
        if (url === "/api/manager/openlist/fs/list") {
          return Promise.resolve(
            envelope({
              content: [
                {
                  name: "Music",
                  size: 0,
                  is_dir: true,
                  modified: "",
                  created: "",
                  sign: "",
                  thumb: "",
                  type: 1,
                  hashinfo: "",
                },
              ],
              total: 1,
            }),
          )
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    await userEvent.click(await screen.findByRole("button", { name: "Add library" }))
    const dialog = await screen.findByRole("dialog", { name: "Add library" })
    await userEvent.type(screen.getByLabelText("Library name"), "Music")
    await userEvent.click(screen.getByRole("switch", { name: "Video screenshot covers" }))
    await userEvent.clear(screen.getByLabelText("Capture position"))
    const contentTypeSelect = dialog.querySelector("select")
    expect(contentTypeSelect).not.toBeNull()
    fireEvent.change(contentTypeSelect!, { target: { value: "music" } })
    expect(
      screen.queryByRole("switch", { name: "Hide cast & crew without profile images" }),
    ).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Select OpenList folders" }))
    await userEvent.click(await screen.findByRole("button", { name: "Select folder Music" }))
    await userEvent.click(screen.getByRole("button", { name: "Use selected folders" }))
    await userEvent.click(screen.getByRole("button", { name: "Save library" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-libraries",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            name: "Music",
            content_type: "music",
            root_paths: ["/Music"],
            video_screenshot_enabled: false,
            video_screenshot_percent: 10,
            playback_completion_percent: 90,
            hide_people_without_images: false,
          }),
        }),
      ),
    )
  })

  it.each([false, true])("renders mixed person works, pagination and episode navigation (parent lookup fails: %s)", async (parentLookupFails) => {
    const onPlay = vi.fn()
    let failNextPage = parentLookupFails
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") {
        return Promise.resolve(envelope({ items: [tvLibrary], total: 1 }))
      }
	  if (url.startsWith("/api/manager/media-library/items/21/people?")) {
		return Promise.resolve(
		  envelope({
			items: [
			  {
				id: 98,
				name: "Patricia Arquette",
				type: "Actor",
				role: "Harmony Cobel",
				sort_order: 0,
			  },
			],
			total: 1,
		  }),
		)
	  }
      if (url === "/api/manager/media-library/items/22") {
        return Promise.resolve(parentLookupFails ? envelope(null, 404) : envelope(season))
      }
      if (url === "/api/manager/media-library/items/21") {
        return Promise.resolve(envelope(series))
      }
      if (url.startsWith("/api/manager/media-library/items/23/people?")) {
        return Promise.resolve(
          envelope({
            items: [
              {
                id: 99,
                name: "Adam Scott",
                type: "Actor",
                role: "Mark Scout",
                sort_order: 0,
              },
            ],
            total: 1,
          }),
        )
      }
      if (url.startsWith("/api/manager/media-library/items?")) {
		if (url.includes("person_id=99")) {
		  const start = new URL(url, "http://localhost").searchParams.get("start")
          if (start === "2" && failNextPage) {
            failNextPage = false
            return Promise.resolve(envelope(null, 500))
          }
          return Promise.resolve(envelope({
            items: start === "2" ? [{ ...episode, series_id: series.id, series_name: series.name }] : [movie, series],
            total: 3,
          }))
		}
        if (url.includes("parent_id=21")) {
          return Promise.resolve(envelope({ items: [season], total: 1 }))
        }
        if (url.includes("parent_id=22")) {
          return Promise.resolve(envelope({ items: [episode], total: 1 }))
        }
        return Promise.resolve(envelope({ items: [series], total: 1 }))
      }
      return Promise.resolve(envelope(null, 404))
    })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={onPlay} />,
    )

    const seriesButton = await screen.findByRole("button", { name: "Severance" })
    expect(
      seriesButton.querySelector('[data-media-orientation="portrait"]'),
    ).toBeInTheDocument()
    await userEvent.click(seriesButton)

    const seasonSelect = await screen.findByRole("combobox", { name: "Season 1" })
    expect(seasonSelect).toBeInTheDocument()
	expect(await screen.findByText("Patricia Arquette")).toBeInTheDocument()
    expect(
      document.querySelector('[data-media-detail] [data-media-orientation="portrait"]'),
    ).toBeInTheDocument()
    const detail = document.querySelector('[data-media-detail]')
    expect(detail).toHaveClass("-mx-5", "-my-7", "lg:-mx-8", "lg:-my-9")
    expect(detail?.querySelector('[data-media-backdrop]')).toHaveClass("inset-0")
    expect(screen.queryByPlaceholderText("Search current media library")).not.toBeInTheDocument()

    const episodeButton = await screen.findByRole("button", {
      name: "S01E01 Good News About Hell",
    })
    const episodeArtwork = episodeButton.closest("article")?.querySelector(
      '[data-media-orientation="landscape"]',
    )
    expect(episodeArtwork).toBeInTheDocument()
    expect(episodeArtwork?.querySelector("img")).toHaveAttribute(
      "src",
      episode.image_url,
    )

    await userEvent.click(episodeButton)
    expect(onPlay).not.toHaveBeenCalled()
    expect(await screen.findByText("Episode information")).toBeInTheDocument()
    expect(screen.getByText("Adam Scott")).toBeInTheDocument()
		expect(screen.getByText("OpenList location")).toBeInTheDocument()

		await userEvent.click(screen.getByRole("button", { name: /Adam Scott/ }))
		expect(await screen.findByText("Adam Scott's local works")).toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Severance" })).toBeInTheDocument()
        await userEvent.click(screen.getByRole("button", { name: "Load more" }))
        if (parentLookupFails) {
          expect(await screen.findByText("Unable to load local works.")).toBeInTheDocument()
          expect(screen.getByRole("button", { name: "Severance" })).toBeInTheDocument()
          await userEvent.click(screen.getByRole("button", { name: "Retry" }))
        }
        const guestCard = await screen.findByRole("button", { name: "Severance · S01E01 · Good News About Hell" })
        expect(guestCard.querySelector('[data-media-orientation="landscape"]')).toBeInTheDocument()
        expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument()
		await userEvent.click(screen.getByRole("button", { name: "Back to episode" }))
		expect(await screen.findByText("Episode information")).toBeInTheDocument()
        await userEvent.click(screen.getByRole("button", { name: /Adam Scott/ }))
        await userEvent.click(await screen.findByRole("button", { name: "Severance · S01E01 · Good News About Hell" }))
        expect(await screen.findByText("Episode information")).toBeInTheDocument()
        expect(onPlay).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole("button", { name: parentLookupFails ? "Play" : "Play Good News About Hell" }))
    expect(onPlay).toHaveBeenCalledWith(expect.objectContaining({ id: episode.id }))
  })

  it("allows administrators to edit indexed media metadata", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (
          url === "/api/manager/media-library/items/11/metadata" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(
            envelope({
              ...movie,
              name: "Dune: Part Two",
              sort_name: "Dune: Part Two",
            }),
          )
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [movie], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })
    const user = userEvent.setup()

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    await user.click(await screen.findByRole("button", { name: "Dune" }))
    await user.click(screen.getByRole("button", { name: "Edit metadata" }))
    expect(await screen.findByRole("dialog", { name: "Edit metadata" })).toBeInTheDocument()
    await user.clear(screen.getByLabelText("Title"))
    await user.type(screen.getByLabelText("Title"), "Dune: Part Two")
    await user.click(screen.getByRole("button", { name: "Save metadata" }))

    await waitFor(() => {
      const updateCall = fetchMock.mock.calls.find(
        ([request, init]) =>
          String(request) === "/api/manager/media-library/items/11/metadata" &&
          (init as RequestInit | undefined)?.method === "PUT",
      )
      expect(updateCall).toBeDefined()
      expect(JSON.parse(String((updateCall?.[1] as RequestInit).body))).toMatchObject({
        name: "Dune: Part Two",
        sort_name: "Dune",
        runtime_ticks: 0,
      })
    })
    expect(await screen.findByRole("heading", { name: "Dune: Part Two" })).toBeInTheDocument()
  })

  it("captures a video screenshot without closing the metadata editor", async () => {
    const capturedMovie = {
      ...movie,
      image_url:
        "/api/manager/media-library/items/11/images/primary?tag=captured-frame",
      image_source_url: "",
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (
          url === "/api/manager/media-library/items/11/images/primary/capture" &&
          init?.method === "POST"
        ) {
          return Promise.resolve(envelope(capturedMovie))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [movie], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })
    const user = userEvent.setup()

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    await user.click(await screen.findByRole("button", { name: "Dune" }))
    await user.click(screen.getByRole("button", { name: "Edit metadata" }))
    await user.click(
      screen.getByRole("button", { name: "Capture video screenshot" }),
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-library/items/11/images/primary/capture",
        expect.objectContaining({ method: "POST" }),
      ),
    )
    expect(screen.getByRole("dialog", { name: "Edit metadata" })).toBeInTheDocument()
    expect(
      document.querySelector(
        'img[src="/api/manager/media-library/items/11/images/primary?tag=captured-frame"]',
      ),
    ).toBeInTheDocument()
  })

  it("updates movie metadata from the TMDB ID input", async () => {
    const refreshedMovie = {
      ...movie,
      name: "Dune: Part Two",
      sort_name: "Dune: Part Two",
      tmdb_id: 693134,
      overview: "Paul Atreides unites with Chani.",
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (
          url === "/api/manager/media-library/items/11/metadata/refresh" &&
          init?.method === "POST"
        ) {
          return Promise.resolve(envelope(refreshedMovie))
        }
        if (
          url === "/api/manager/media-library/items/11/metadata" &&
          init?.method === "PUT"
        ) {
          return Promise.resolve(
            envelope({
              ...movie,
              tmdb_id: 693134,
            }),
          )
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [movie], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })
    const user = userEvent.setup()

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    await user.click(await screen.findByRole("button", { name: "Dune" }))
    await user.click(screen.getByRole("button", { name: "Edit metadata" }))
    expect(screen.queryByLabelText("IMDb ID")).not.toBeInTheDocument()
    await user.clear(screen.getByLabelText("TMDB ID"))
    await user.type(screen.getByLabelText("TMDB ID"), "693134")
    await user.click(screen.getByRole("button", { name: "Save metadata" }))

    await waitFor(() => {
      const updateCall = fetchMock.mock.calls.find(
        ([request, init]) =>
          String(request) === "/api/manager/media-library/items/11/metadata" &&
          (init as RequestInit | undefined)?.method === "PUT",
      )
      const refreshCall = fetchMock.mock.calls.find(
        ([request, init]) =>
          String(request) === "/api/manager/media-library/items/11/metadata/refresh" &&
          (init as RequestInit | undefined)?.method === "POST",
      )
      expect(updateCall).toBeDefined()
      expect(refreshCall).toBeDefined()
      expect(JSON.parse(String((updateCall?.[1] as RequestInit).body))).toMatchObject({ tmdb_id: 693134, metadata_source: "auto" })
      expect(JSON.parse(String((refreshCall?.[1] as RequestInit).body))).toEqual({ metadata_source: "auto" })
    })
    expect(await screen.findByRole("heading", { name: "Dune: Part Two" })).toBeInTheDocument()
  })

  it("opens an in-progress episode at its series season and episode", async () => {
    const resumeEpisode = {
      ...episode,
      runtime_ticks: 840_000_000,
      user_data: {
        ...episode.user_data,
        playback_position_ticks: 420_000_000,
      },
    }
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") {
        return Promise.resolve(envelope({ items: [tvLibrary], total: 1 }))
      }
      if (url === "/api/manager/media-library/items/22") {
        return Promise.resolve(envelope(season))
      }
      if (url === "/api/manager/media-library/items/21") {
        return Promise.resolve(envelope(series))
      }
	  if (url === "/api/manager/media-library/continue-watching?start=0&limit=24") {
		return Promise.resolve(envelope({ items: [{ item: resumeEpisode, source: "resume" }], total: 1 }))
	  }
      if (url.startsWith("/api/manager/media-library/items?")) {
        if (url.includes("parent_id=21")) {
          return Promise.resolve(envelope({ items: [season], total: 1 }))
        }
        if (url.includes("parent_id=22")) {
          return Promise.resolve(envelope({ items: [episode], total: 1 }))
        }
        return Promise.resolve(envelope({ items: [series], total: 1 }))
      }
      return Promise.resolve(envelope(null, 404))
    })

	const onPlay = vi.fn()
    renderWithProviders(
	  <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={onPlay} />,
	)

    expect(
      await screen.findByRole("progressbar", { name: "Watched progress for Good News About Hell" }),
    ).toHaveAttribute("aria-valuenow", "50")

    await userEvent.click(
      await screen.findByRole("button", { name: "S01E01 Good News About Hell" }),
    )
	expect(onPlay).toHaveBeenCalledWith(resumeEpisode)

	await userEvent.click(screen.getByRole("button", { name: "Actions for Good News About Hell" }))
	await userEvent.click(screen.getByRole("menuitem", { name: "View details" }))

    expect(await screen.findByRole("combobox", { name: "Season 1" })).toBeInTheDocument()
    expect(
      screen.getByRole("button", { name: "S01E01 Good News About Hell" }),
    ).toHaveAttribute("aria-current", "true")
    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(
      expect.arrayContaining([
        "/api/manager/media-library/items/22",
        "/api/manager/media-library/items/21",
      ]),
    )
  })

  it("opens the complete library from its homepage title", async () => {
    const secondMovie = {
      ...movie,
      id: 12,
      path: "/Media/Movies/Dune Part Two.mkv",
      media_path: "/Media/Movies/Dune Part Two.mkv",
      name: "Dune: Part Two",
      sort_name: "Dune: Part Two",
      production_year: 2024,
    }
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(
            url.includes("start=1")
              ? envelope({ items: [secondMovie], total: 2 })
              : envelope({ items: [movie], total: 2 }),
          )
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />,
    )

    expect(await screen.findByRole("button", { name: "Dune" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument()
    await userEvent.click(screen.getAllByRole("button", { name: "Movies" })[1])

    expect(
      await screen.findByRole("button", { name: "Dune: Part Two" }),
    ).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes("start=1")),
    ).toBe(true)
  })

  it("keeps the library read-only for media users without touching OpenList APIs", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL) => {
        const url = String(input)
        if (url === "/api/manager/media-libraries") {
          return Promise.resolve(envelope({ items: [library], total: 1 }))
        }
        if (url.startsWith("/api/manager/media-library/items?")) {
          return Promise.resolve(envelope({ items: [movie], total: 1 }))
        }
        return Promise.resolve(envelope(null, 404))
      })

    renderWithProviders(
      <MediaLibraryPage
        isAdmin={false}
        onTaskCreated={vi.fn()}
        onViewTask={vi.fn()}
        onPlay={vi.fn()}
      />,
    )

    expect(await screen.findByRole("button", { name: "Dune" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Add library" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Scan library" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Dune" }))
    expect(screen.queryByRole("button", { name: "Edit metadata" })).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(([input]) =>
        String(input).startsWith("/api/manager/openlist/"),
      ),
    ).toBe(false)
    expect(
      fetchMock.mock.calls.some(([input]) =>
        String(input).startsWith("/api/manager/tasks/"),
      ),
    ).toBe(false)
  })

    it("hides metadata editing for media users", async () => {
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-libraries") {
        return Promise.resolve(envelope({ items: [library], total: 1 }))
      }
      if (url.startsWith("/api/manager/media-library/items?")) {
        return Promise.resolve(envelope({ items: [movie], total: 1 }))
      }
      return Promise.resolve(envelope(null, 404))
    })

    renderWithProviders(
      <MediaLibraryPage
        isAdmin={false}
        onTaskCreated={vi.fn()}
        onViewTask={vi.fn()}
        onPlay={vi.fn()}
      />,
    )

    await userEvent.click(await screen.findByRole("button", { name: "Dune" }))
        expect(screen.queryByRole("button", {name: "Edit metadata"})).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Add library" })).not.toBeInTheDocument()
  })
})

it.each(["resume", "next_up"] as const)("hides %s without clearing playback progress", async (source) => {
  let hidden = false
  const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input, init) => {
    const url = String(input)
    if (url.endsWith("/resume-visibility")) {
      hidden = true
      return Promise.resolve(envelope({ ...movie.user_data, hide_from_resume: true, playback_position_ticks: 4200000 }))
    }
    if (url.includes("/continue-watching?")) return Promise.resolve(envelope({ items: hidden ? [] : [{ item: movie, source }], total: hidden ? 0 : 1 }))
    if (url === "/api/manager/media-libraries") return Promise.resolve(envelope({ items: [library], total: 1 }))
    if (init?.method === "DELETE") throw new Error("must not clear playback progress")
    return Promise.resolve(envelope({ items: [], total: 0 }))
  })
  renderWithProviders(<MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />)
  await userEvent.click(await screen.findByRole("button", { name: "Actions for Dune" }))
  await userEvent.click(screen.getByRole("menuitem", { name: "Remove from Continue Watching" }))
  await waitFor(() => expect(screen.queryByRole("button", { name: "Actions for Dune" })).not.toBeInTheDocument())
  expect(fetchMock).toHaveBeenCalledWith("/api/manager/media-library/items/11/resume-visibility", expect.objectContaining({ method: "PUT", body: JSON.stringify({ hidden: true }) }))
})


it("keeps a next-up card when hiding fails and shows a localized error", async () => {
  window.localStorage.setItem("openlist2emby.language", "zh")
  const errorToast = vi.spyOn(toast, "error")
  vi.spyOn(window, "fetch").mockImplementation((input) => {
    const url = String(input)
    if (url.endsWith("/resume-visibility")) return Promise.resolve(envelope(null, 503))
    if (url.includes("/continue-watching?")) return Promise.resolve(envelope({ items: [{ item: movie, source: "next_up" }], total: 1 }))
    if (url === "/api/manager/media-libraries") return Promise.resolve(envelope({ items: [library], total: 1 }))
    return Promise.resolve(envelope({ items: [], total: 0 }))
  })
  renderWithProviders(<MediaLibraryPage onTaskCreated={vi.fn()} onViewTask={vi.fn()} onPlay={vi.fn()} />)
  const trigger = await screen.findByRole("button", { name: "Dune 的操作" })
  expect(trigger).toHaveClass("size-11")
  await userEvent.click(trigger)
  const action = screen.getByRole("menuitem", { name: "移出继续观看" })
  expect(screen.getByRole("menu")).toHaveClass("max-w-[calc(100vw-2rem)]")
  await userEvent.click(action)
  await waitFor(() => expect(errorToast).toHaveBeenCalledWith("无法从继续观看中移除"))
  expect(screen.getByRole("button", { name: "Dune 的操作" })).toBeInTheDocument()
})
