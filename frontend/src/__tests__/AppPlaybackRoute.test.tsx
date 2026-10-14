import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { App } from "@/App"
import { renderWithProviders } from "@/test/render"

vi.mock("@/features/media/MediaLibraryPage", () => ({
  MediaLibraryPage: ({ onPlay }: { onPlay: (item: { id: number }) => void }) => (
    <button onClick={() => onPlay({ id: 11 })}>Open playback</button>
  ),
}))

vi.mock("@/features/media/MediaPlaybackPage", () => ({
  default: ({ itemID, onBack }: { itemID: number; onBack: () => void }) => (
    <main>
      <h1>Playback {itemID}</h1>
      <button onClick={onBack}>Leave playback</button>
    </main>
  ),
}))

const fetchMock = vi.fn()
vi.stubGlobal("fetch", fetchMock)

function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200, message: "success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })
}

function mockAppRequests() {
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url === "/api/manager/status") {
      return Promise.resolve(
        envelope({
          setup_required: false,
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          media_library_enabled: true,
        }),
      )
    }
    if (url === "/api/manager/auth/me") {
      return Promise.resolve(
        envelope({
          username: "manager",
          expires_at: "2026-07-31T12:00:00Z",
          is_admin: true,
        }),
      )
    }
    if (url.startsWith("/api/manager/tasks/runs?")) {
      return Promise.resolve(envelope({ items: [], total: 0, page: 1, per_page: 1 }))
    }
    return Promise.reject(new Error(`unexpected request: ${url}`))
  })
}

afterEach(() => {
  fetchMock.mockReset()
  window.history.replaceState(null, "", "/")
  window.localStorage.clear()
})

describe("media playback route", () => {
  it("opens outside the app shell and returns with browser history", async () => {
    mockAppRequests()
    const user = userEvent.setup()
    renderWithProviders(<App />)

    await user.click(await screen.findByRole("button", { name: "Media Library" }))
    await user.click(screen.getByRole("button", { name: "Open playback" }))

    expect(await screen.findByRole("heading", { name: "Playback 11" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/media/play/11")
    expect(screen.queryByRole("button", { name: "Files" })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Leave playback" }))
    await waitFor(() => expect(window.location.pathname).toBe("/"))
    expect(await screen.findByRole("button", { name: "Open playback" })).toBeInTheDocument()
  })

  it("falls back to the media library when a deep link has no app history", async () => {
    window.history.replaceState(null, "", "/media/play/11")
    mockAppRequests()
    const user = userEvent.setup()
    renderWithProviders(<App />)

    expect(await screen.findByRole("heading", { name: "Playback 11" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Leave playback" }))

    expect(window.location.pathname).toBe("/")
    expect(await screen.findByRole("button", { name: "Open playback" })).toBeInTheDocument()
  })
})
