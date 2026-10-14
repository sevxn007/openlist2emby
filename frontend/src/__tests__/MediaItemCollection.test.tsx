import { fireEvent, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { MediaRail } from "@/features/media/MediaItemCollection"
import type { MediaLibraryItem } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

const originalViewportWidth = window.innerWidth

afterEach(() => {
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: originalViewportWidth,
  })
})

describe("MediaRail", () => {
  it("keeps all items reachable through horizontal scrolling", async () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 1024,
    })
    const items = Array.from({ length: 6 }, (_, index): MediaLibraryItem => ({
      id: index + 1,
      library_id: 7,
      kind: "movie",
      path: `/movies/movie-${index + 1}.mkv`,
      media_path: `/movies/movie-${index + 1}.mkv`,
      name: `Movie ${index + 1}`,
      sort_name: `Movie ${index + 1}`,
      container: "mkv",
      size_bytes: 1024,
      user_data: {
        playback_position_ticks: 0,
        played: false,
        play_count: 0,
        favorite: false,
      },
      created_at: "2026-08-16T00:00:00Z",
      updated_at: "2026-08-16T00:00:00Z",
    }))

    renderWithProviders(
      <MediaRail title="Movies" items={items} onOpen={vi.fn()} />,
    )

    expect(screen.getAllByRole("button", { name: /^Movie \d$/ })).toHaveLength(6)
    const rail = screen.getByRole("button", { name: "Movie 1" }).closest(".overflow-x-auto")
    expect(rail).not.toBeNull()
    if (!(rail instanceof HTMLElement)) throw new Error("Missing media rail")
    const scrollBy = vi.fn()
    Object.defineProperties(rail, {
      clientWidth: { configurable: true, value: 800 },
      scrollWidth: { configurable: true, value: 1600 },
      scrollLeft: { configurable: true, writable: true, value: 0 },
      scrollBy: { configurable: true, value: scrollBy },
    })
    fireEvent.scroll(rail)
    expect(screen.queryByRole("button", { name: "Previous items in Movies" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Next items in Movies" }))
    expect(scrollBy).toHaveBeenCalledWith({ left: 640, behavior: "smooth" })
    rail.scrollLeft = 800
    fireEvent.scroll(rail)
    expect(screen.queryByRole("button", { name: "Next items in Movies" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Previous items in Movies" }))
    expect(scrollBy).toHaveBeenLastCalledWith({ left: -640, behavior: "smooth" })
  })
})
