import { act, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  MEDIA_SEARCH_HISTORY_STORAGE_KEY,
  MediaPicker,
  MediaSearchPanel,
} from "@/features/files/MediaSearchPanel"
import { buildMovieRename } from "@/features/files/mediaNaming"
import { renderWithProviders } from "@/test/render"

const fetchMock = vi.fn()

vi.stubGlobal("fetch", fetchMock)

function mockEnvelope(data: unknown, status = 200, message = "success") {
  return new Response(JSON.stringify({ code: status, message, data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const movieResult = {
  id: 438631,
  media_type: "movie" as const,
  title: "Dune",
  original_title: "Dune",
  overview: "A noble family becomes embroiled in a war for control.",
  poster_path: "/dune.jpg",
  release_date: "2021-10-22",
  vote_average: 8.1,
}

const tvResult = {
  id: 95396,
  media_type: "tv" as const,
  title: "Severance",
  name: "Severance",
  original_title: "Severance",
  overview: "Employees have their memories divided.",
  poster_path: "/severance.jpg",
  first_air_date: "2022-02-18",
  vote_average: 8.4,
}

beforeEach(() => {
  fetchMock.mockReset()
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  window.localStorage.clear()
})

describe("MediaSearchPanel", () => {
  it("labels each provider when different sources return the same title and ID", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [
      { ...movieResult, source: "tmdb" },
      { ...movieResult, source: "tvdb" },
    ] }))
    const onMovieSelect = vi.fn()
    renderWithProviders(<MediaPicker source="auto" onMovieSelect={onMovieSelect} onEpisodeSelect={vi.fn()} />)

    await userEvent.type(screen.getByLabelText("Movie or TV title"), "Dune")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))
    const results = await screen.findAllByRole("button", { name: "Select Dune" })
    expect(results).toHaveLength(2)
    expect(within(results[0]).getByText("TMDB")).toHaveAttribute("data-slot", "badge")
    expect(within(results[1]).getByText("TVDB")).toHaveAttribute("data-slot", "badge")
    await userEvent.click(results[1])
    expect(onMovieSelect).toHaveBeenCalledWith(expect.objectContaining({ source: "tvdb", id: movieResult.id }))
  })

  it("selects a movie and preserves the original file extension", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [movieResult] }))
    const onSelectName = vi.fn()

    renderWithProviders(
      <MediaSearchPanel
        originalName="Dune.2021.2160p.mkv"
        isDirectory={false}
        onSelectName={onSelectName}
      />,
    )

    await userEvent.click(screen.getByRole("button", { name: "Media search" }))
    await userEvent.type(screen.getByLabelText("Movie or TV title"), "Dune")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))
    await userEvent.click(await screen.findByRole("button", { name: "Select Dune" }))

    expect(onSelectName).toHaveBeenCalledWith("Dune.mkv")
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media/search?query=Dune&language=en-US",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("loads TV seasons and episodes before generating an episode name", async () => {
    fetchMock
      .mockResolvedValueOnce(mockEnvelope({ results: [tvResult] }))
      .mockResolvedValueOnce(
        mockEnvelope({
          id: 95396,
          media_type: "tv",
          title: "Severance",
          vote_average: 8.4,
          number_of_seasons: 2,
          seasons: [
            {
              id: 145352,
              air_date: "2022-02-18",
              episode_count: 9,
              name: "Season 1",
              overview: "",
              poster_path: "/season-1.jpg",
              season_number: 1,
              vote_average: 8.3,
            },
          ],
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          id: 145352,
          media_type: "tv",
          vote_average: 8.3,
          season_number: 1,
          episodes: [
            {
              id: 3471598,
              air_date: "2022-02-18",
              episode_number: 1,
              name: "Good News About Hell",
              overview: "Mark starts a new day at Lumon.",
              runtime: 57,
              season_number: 1,
              still_path: "/episode-1.jpg",
              vote_average: 8.1,
              vote_count: 42,
            },
          ],
        }),
      )
    const onSelectName = vi.fn()

    renderWithProviders(
      <MediaSearchPanel
        originalName="show-source.mp4"
        isDirectory={false}
        onSelectName={onSelectName}
      />,
    )

    await userEvent.click(screen.getByRole("button", { name: "Media search" }))
    await userEvent.type(screen.getByLabelText("Movie or TV title"), "Severance")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))
    await userEvent.click(await screen.findByRole("button", { name: "Select Severance" }))
    await userEvent.click(await screen.findByRole("button", { name: "Select Season 1" }))
    const episodeButton = await screen.findByRole("button", { name: "Select episode 1" })
    expect(episodeButton).toHaveClass("min-h-28", "shrink-0")
    await userEvent.click(episodeButton)

    expect(onSelectName).toHaveBeenCalledWith(
      "Severance - S01E01 - Good News About Hell.mp4",
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/manager/media/detail?type=tv&id=95396&language=en-US",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/manager/media/detail?type=tv&id=95396&language=en-US&season=1",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("uses the current Chinese locale and renders an empty result", async () => {
    window.localStorage.setItem("openlist2emby.language", "zh")
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [] }))

    renderWithProviders(
      <MediaSearchPanel
        originalName="电影.mkv"
        isDirectory={false}
        onSelectName={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole("button", { name: "媒体搜索" }))
    await userEvent.type(screen.getByLabelText("电影或电视剧名称"), "沙丘")
    await userEvent.click(screen.getByRole("button", { name: "搜索" }))

    expect(await screen.findByText("未找到匹配的电影或电视剧")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media/search?query=%E6%B2%99%E4%B8%98&language=zh-CN",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("renders loading and API error states", async () => {
    let resolveRequest: ((response: Response) => void) | undefined
    fetchMock.mockReturnValueOnce(
      new Promise<Response>((resolve) => {
        resolveRequest = resolve
      }),
    )

    renderWithProviders(
      <MediaSearchPanel
        originalName="movie.mkv"
        isDirectory={false}
        onSelectName={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole("button", { name: "Media search" }))
    await userEvent.type(screen.getByLabelText("Movie or TV title"), "Unknown")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))
    expect(screen.getByRole("button", { name: "Search" })).toBeDisabled()

    await act(async () => {
      resolveRequest?.(mockEnvelope(null, 502, "tmdb request failed"))
    })
    expect(await screen.findByText("tmdb request failed")).toBeInTheDocument()
  })

  it("does not append an extension when renaming a folder", () => {
    expect(buildMovieRename(movieResult, "Movies", true)).toBe("Dune")
  })

  it("filters movies when used for batch episode selection", async () => {
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [movieResult, tvResult] }))

    renderWithProviders(<MediaPicker tvOnly onEpisodeSelect={vi.fn()} />)
    await userEvent.type(screen.getByLabelText("Movie or TV title"), "Dune")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))

    expect(await screen.findByRole("button", { name: "Select Severance" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Select Dune" })).not.toBeInTheDocument()
  })

  it("shows the latest ten search history entries and reuses one", async () => {
    const history = Array.from({ length: 12 }, (_, index) => `Show ${11 - index}`)
    window.localStorage.setItem(
      MEDIA_SEARCH_HISTORY_STORAGE_KEY,
      JSON.stringify(history),
    )
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [tvResult] }))

    renderWithProviders(
      <MediaPicker
        tvOnly
        showSearchHistory
        onEpisodeSelect={vi.fn()}
      />,
    )

    expect(screen.getByRole("button", { name: "Search for Show 11" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Search for Show 2" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Search for Show 1" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Search for Show 0" })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole("button", { name: "Search for Show 11" }))

    expect(screen.getByLabelText("Movie or TV title")).toHaveValue("Show 11")
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/media/search?query=Show+11&language=en-US",
      expect.objectContaining({ credentials: "include" }),
    )
  })

  it("stores new searches at the front and caps history at ten", async () => {
    const history = Array.from({ length: 9 }, (_, index) => `Old ${8 - index}`)
    window.localStorage.setItem(
      MEDIA_SEARCH_HISTORY_STORAGE_KEY,
      JSON.stringify(history),
    )
    fetchMock.mockResolvedValueOnce(mockEnvelope({ results: [] }))

    renderWithProviders(
      <MediaPicker
        tvOnly
        showSearchHistory
        onEpisodeSelect={vi.fn()}
      />,
    )

    await userEvent.type(screen.getByLabelText("Movie or TV title"), "New show")
    await userEvent.click(screen.getByRole("button", { name: "Search" }))

    expect(JSON.parse(window.localStorage.getItem(MEDIA_SEARCH_HISTORY_STORAGE_KEY) ?? "[]"))
      .toEqual(["New show", ...history])
  })
})
