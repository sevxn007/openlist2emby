import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { App } from "@/App"
import { renderWithProviders } from "@/test/render"

vi.mock("@/features/files/FileBrowser", () => ({
  FileBrowser: () => <main>File browser should not mount</main>,
}))

vi.mock("@/features/media/MediaLibraryPage", () => ({
  MediaLibraryPage: ({ isAdmin }: { isAdmin?: boolean }) => (
    <main>{isAdmin ? "Media manager" : "Media only"}</main>
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

function mockMediaUser(mediaLibraryEnabled: boolean) {
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url === "/api/manager/status") {
      return Promise.resolve(
        envelope({
          setup_required: false,
          media_library_enabled: mediaLibraryEnabled,
        }),
      )
    }
    if (url === "/api/manager/auth/me") {
      return Promise.resolve(
        envelope({
          id: 2,
          username: "viewer",
          role: "media",
          is_admin: false,
          expires_at: "2026-08-01T12:00:00Z",
        }),
      )
    }
    if (url === "/api/manager/auth/logout") {
      return Promise.resolve(envelope(null))
    }
    return Promise.reject(new Error(`unexpected request: ${url}`))
  })
}

afterEach(() => {
  fetchMock.mockReset()
  window.localStorage.clear()
  window.history.replaceState(null, "", "/")
})

describe("media-user application access", () => {
  it.each(["files", "media", "tasks", "users", "settings", "sync", "music-search"])(
    "restricts the %s link to the read-only media library",
    async (section) => {
    window.history.replaceState(null, "", `/?section=${section}`)
    window.localStorage.setItem("openlist2emby.language", "en")
    mockMediaUser(true)

    renderWithProviders(<App />)

    expect(await screen.findByText("Media only")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Media Library" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Files" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Settings" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Tasks" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Users" })).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.map(([input]) => String(input)),
    ).toEqual(["/api/manager/status", "/api/manager/auth/me"])
    },
  )

  it("does not fall back to OpenList files when the media library is disabled", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    mockMediaUser(false)

    renderWithProviders(<App />)

    expect(
      await screen.findByText("Media library unavailable"),
    ).toBeInTheDocument()
    expect(screen.queryByText("File browser should not mount")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Files" })).not.toBeInTheDocument()
  })

  it("keeps an exit action available for media users", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    mockMediaUser(true)
    const user = userEvent.setup()

    renderWithProviders(<App />)

    const logoutButtons = await screen.findAllByRole("button", { name: "Logout" })
    expect(logoutButtons.length).toBeGreaterThan(0)
    await user.click(logoutButtons[0])

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/auth/logout",
        expect.objectContaining({ method: "POST" }),
      )
    })
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument()
  })
})
