import { act, fireEvent, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import MediaPlaybackPage from "@/features/media/MediaPlaybackPage"
import { renderWithProviders } from "@/test/render"

const playerMock = vi.hoisted(() => ({
  currentTime: 0,
  paused: false,
  play: vi.fn(() => Promise.resolve()),
}))

vi.mock("@vidstack/react", async () => {
  const React = await import("react")
  return {
    MediaPlayer: React.forwardRef(
      (
        props: {
          children: React.ReactNode
          onCanPlay?: () => void
          onTimeUpdate?: (detail: { currentTime: number }) => void
          onPause?: () => void
          onEnded?: () => void
          onError?: () => void
          viewType?: string
        },
        ref: React.ForwardedRef<typeof playerMock>,
      ) => {
        React.useImperativeHandle(ref, () => playerMock)
        return (
          <div data-testid="media-player" data-view-type={props.viewType}>
            {props.children}
            <button onClick={props.onCanPlay}>can play</button>
            <button onClick={() => props.onTimeUpdate?.({ currentTime: 55 })}>
              time update
            </button>
            <button onClick={props.onPause}>pause media</button>
            <button onClick={props.onEnded}>end media</button>
            <button onClick={props.onError}>error media</button>
          </div>
        )
      },
    ),
    MediaProvider: () => <div data-testid="media-provider" />,
  }
})

vi.mock("@vidstack/react/player/layouts/default", () => ({
  DefaultAudioLayout: () => <div>audio controls</div>,
  DefaultVideoLayout: () => <div>video controls</div>,
  defaultLayoutIcons: {},
}))

const fetchMock = vi.fn()
vi.stubGlobal("fetch", fetchMock)

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: status, message: "success", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function mediaItem(kind: "movie" | "audio" = "movie") {
  return {
    id: 11,
    library_id: 7,
    kind,
    path: kind === "audio" ? "/Music/Track.mp3" : "/Movies/Dune.mp4",
    media_path: kind === "audio" ? "/Music/Track.mp3" : "/Movies/Dune.mp4",
    name: kind === "audio" ? "Track" : "Dune",
    sort_name: kind === "audio" ? "Track" : "Dune",
    container: kind === "audio" ? "mp3" : "mp4",
    size_bytes: 1024,
    runtime_ticks: 1_000_000_000,
    image_url: kind === "audio" ? "https://images.example/track.jpg" : undefined,
	album: kind === "audio" ? "Album" : undefined,
	album_artist: kind === "audio" ? "Artist" : undefined,
	artists: kind === "audio" ? ["Artist"] : undefined,
	lyrics: kind === "audio" ? "[00:01.00]Original line" : undefined,
	translated_lyrics: kind === "audio" ? "[00:01.00]Translated line" : undefined,
    user_data: {
      playback_position_ticks: 420_000_000,
      played: false,
      play_count: 1,
      favorite: true,
    },
    created_at: "2026-07-31T00:00:00Z",
    updated_at: "2026-07-31T00:00:00Z",
  }
}

function mkvMediaItem() {
  return {
    ...mediaItem(),
    path: "/Movies/Dune.mkv",
    media_path: "/Movies/Dune.mkv",
    container: "mkv",
  }
}

afterEach(() => {
  fetchMock.mockReset()
  playerMock.currentTime = 0
  playerMock.paused = false
  playerMock.play.mockClear()
  window.localStorage.clear()
})

describe("MediaPlaybackPage", () => {
  it("resumes video and persists paused and completed progress", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/media-library/items/11") {
        return Promise.resolve(envelope(mediaItem()))
      }
      if (url.endsWith("/playback")) {
        return Promise.resolve(envelope(mediaItem().user_data))
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<MediaPlaybackPage itemID={11} onBack={vi.fn()} />)

    expect(await screen.findByText("video controls")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "can play" }))
    expect(playerMock.currentTime).toBe(42)
    expect(playerMock.play).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole("button", { name: "time update" }))
    fireEvent.click(screen.getByRole("button", { name: "pause media" }))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-library/items/11/playback",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ position_ticks: 550_000_000, stopped: false }),
        }),
      )
    })

    fireEvent.click(screen.getByRole("button", { name: "end media" }))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media-library/items/11/playback",
        expect.objectContaining({
          body: JSON.stringify({ position_ticks: 550_000_000, stopped: true }),
        }),
      )
    })
  })

  it("renders the audio layout and a localized player error", async () => {
    window.localStorage.setItem("openlist2emby.language", "zh")
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      if (String(input) === "/api/manager/media-library/items/11") {
        return Promise.resolve(envelope(mediaItem("audio")))
      }
      return Promise.resolve(envelope(mediaItem("audio").user_data))
    })

    renderWithProviders(<MediaPlaybackPage itemID={11} onBack={vi.fn()} />)

    expect(await screen.findByText("audio controls")).toBeInTheDocument()
    await waitFor(() => {
      expect(window.localStorage.getItem("openlist2emby.audio-player.preferences")).not.toBeNull()
    })
    expect(screen.getByRole("img", { name: "Track" })).toBeInTheDocument()
		expect(screen.getAllByText("Artist · Album")).toHaveLength(2)
	expect(screen.getByText("Original line")).toBeInTheDocument()
    expect(screen.getByText("Translated line")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "can play" }))
    expect(playerMock.currentTime).toBe(42)
    expect(playerMock.play).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: "error media" }))
    expect(
      screen.getByText(
        "当前浏览器不支持此文件格式或编码，请使用 Emby 客户端进行连接播放。",
      ),
    ).toBeInTheDocument()
  })

  it("starts the next episode after the completed-state countdown", async () => {
	const currentEpisode = {
	  ...mediaItem(),
	  kind: "episode",
	  season_number: 1,
	  episode_number: 1,
	  name: "Episode 1",
	}
	const nextEpisode = {
	  ...currentEpisode,
	  id: 12,
	  episode_number: 2,
	  name: "Episode 2",
	  user_data: { ...currentEpisode.user_data, playback_position_ticks: 0 },
	}
	fetchMock.mockImplementation((input: RequestInfo | URL) => {
	  const url = String(input)
	  if (url === "/api/manager/media-library/items/11") return Promise.resolve(envelope(currentEpisode))
	  if (url.endsWith("/playback")) return Promise.resolve(envelope({ ...currentEpisode.user_data, played: true, playback_position_ticks: 0 }))
	  if (url.endsWith("/next-episode")) return Promise.resolve(envelope({ item: nextEpisode }))
	  return Promise.reject(new Error(`unexpected request: ${url}`))
	})
	const onPlayItem = vi.fn()
	renderWithProviders(<MediaPlaybackPage itemID={11} onBack={vi.fn()} onPlayItem={onPlayItem} />)

	expect(await screen.findByText("video controls")).toBeInTheDocument()
	vi.useFakeTimers()
	try {
	fireEvent.click(screen.getByRole("button", { name: "time update" }))
	fireEvent.click(screen.getByRole("button", { name: "end media" }))
	await act(async () => {
		await Promise.resolve()
		await Promise.resolve()
		await Promise.resolve()
	})
	expect(screen.getByText("Next episode")).toBeInTheDocument()
	expect(screen.getByText("Playing in 8s")).toBeInTheDocument()

	  for (let second = 0; second < 8; second += 1) {
		await act(async () => {
		  vi.advanceTimersByTime(1_000)
		})
	  }
	 expect(onPlayItem).toHaveBeenCalledWith(12)
	} finally {
	  vi.useRealTimers()
	}
  })

  it("rejects MKV before mounting the player or requesting the stream", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      if (String(input) === "/api/manager/media-library/items/11") {
        return Promise.resolve(envelope(mkvMediaItem()))
      }
      return Promise.reject(new Error(`unexpected request: ${String(input)}`))
    })

    renderWithProviders(<MediaPlaybackPage itemID={11} onBack={vi.fn()} />)

    expect(
      await screen.findByText(
        "This browser cannot play this file format or codec. Please connect with an Emby client to play it.",
      ),
    ).toBeInTheDocument()
    expect(screen.queryByTestId("media-player")).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([request]) => String(request).endsWith("/stream"))).toBe(false)
    expect(fetchMock.mock.calls.some(([request]) => String(request) === "/api/manager/media-library/items/11")).toBe(true)
    expect(fetchMock.mock.calls.find(([request]) => String(request) === "/api/manager/media-library/items/11")?.[0]).toBe(
      "/api/manager/media-library/items/11",
    )
  })
})
