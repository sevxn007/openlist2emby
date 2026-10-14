import {screen, waitFor} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, describe, expect, it, vi} from "vitest"

import {SettingsPage} from "@/features/settings/SettingsPage"
import {renderWithProviders} from "@/test/render"

const fetchMock = vi.fn()

vi.stubGlobal("fetch", fetchMock)

function mockEnvelope(data: unknown, status = 200, message = "success") {
  return new Response(JSON.stringify({ code: status, message, data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

afterEach(() => {
  fetchMock.mockReset()
  window.localStorage.clear()
})

describe("SettingsPage", () => {
  it("enables the media library from settings", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    const onSaved = vi.fn()
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
          media_library_enabled: false,
          emby_port: 8096,
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
          media_library_enabled: true,
          emby_port: 8096,
        }),
      )

    renderWithProviders(<SettingsPage onSaved={onSaved} />)

    const toggle = await screen.findByRole("switch", {
      name: "Enable media library and Emby API",
    })
    await userEvent.click(toggle)
    await userEvent.click(
      screen.getByRole("button", { name: "Save media library" }),
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        "/api/manager/settings",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            openlist_base_url: "http://openlist.local",
            openlist_admin_key: "",
            media_library_enabled: true,
            emby_port: 8096,
          }),
        }),
      ),
    )
    expect(onSaved).toHaveBeenCalledWith(
      expect.objectContaining({ media_library_enabled: true }),
    )
  })

  it("tests unsaved settings and reports success", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.example",
          username: "admin",
        }),
      )

    renderWithProviders(<SettingsPage onSaved={vi.fn()} />)

    const urlInput = await screen.findByLabelText("OpenList address")
    await waitFor(() => expect(urlInput).toHaveValue("http://openlist.local"))
    await userEvent.clear(urlInput)
    await userEvent.type(urlInput, "http://openlist.example")
    await userEvent.type(screen.getByLabelText("Administrator key"), "new-key")
    await userEvent.click(screen.getByRole("button", { name: "Test connection" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/settings/test",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            openlist_base_url: "http://openlist.example",
            openlist_admin_key: "new-key",
          }),
        }),
      )
    })
    expect(screen.queryByText("Connection failed")).not.toBeInTheDocument()
  })

  it("shows the manager error when the connection test fails", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: false,
          tmdb_api_key: "",
        }),
      )
      .mockResolvedValueOnce(mockEnvelope(null, 400, "openlist administrator key is required"))

    renderWithProviders(<SettingsPage onSaved={vi.fn()} />)

    await userEvent.click(await screen.findByRole("button", { name: "Test connection" }))

    expect(
      await screen.findByText("openlist administrator key is required"),
    ).toBeInTheDocument()
  })

  it("tests an unsaved TMDB key without saving it", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
        }),
      )
      .mockResolvedValueOnce(mockEnvelope({ connected: true }))

    renderWithProviders(<SettingsPage onSaved={vi.fn()} />)

    const tmdbInput = await screen.findByLabelText("TMDB API key")
    await waitFor(() =>
      expect(screen.getByLabelText("OpenList address")).toHaveValue(
        "http://openlist.local",
      ),
    )
    await userEvent.type(tmdbInput, "new-tmdb-key")
    await userEvent.click(screen.getByRole("button", { name: "Test TMDB connection" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/media/test",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ source: "tmdb", tmdb_api_key: "new-tmdb-key", tvdb_api_key: "", tvdb_pin: "" }),
        }),
      )
    })
  })

  it("saves a new TMDB key while keeping masked values out of the form", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
        }),
      )
      .mockResolvedValueOnce(
        mockEnvelope({
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          openlist_admin_key: "********",
          tmdb_api_key_set: true,
          tmdb_api_key: "********",
        }),
      )

    renderWithProviders(<SettingsPage onSaved={vi.fn()} />)

    const tmdbInput = await screen.findByLabelText("TMDB API key")
    await waitFor(() =>
      expect(screen.getByLabelText("OpenList address")).toHaveValue(
        "http://openlist.local",
      ),
    )
    expect(tmdbInput).toHaveValue("")
    expect(tmdbInput).toHaveAttribute("placeholder", "Leave blank to keep current key")
    await userEvent.type(tmdbInput, "replacement-key")
    await userEvent.clear(screen.getByLabelText("OpenList address"))
    await userEvent.type(
      screen.getByLabelText("OpenList address"),
      "http://unsaved-openlist.local",
    )
    await userEvent.click(screen.getByRole("button", { name: "Save metadata settings" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/settings",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            openlist_base_url: "http://openlist.local",
            openlist_admin_key: "",
              tmdb_api_key: "replacement-key", tvdb_api_key: "", tvdb_pin: "", metadata_priority: ["tmdb", "tvdb"],
          }),
        }),
      )
    })
    const storedValues = Array.from(
      { length: window.localStorage.length },
      (_, index) => window.localStorage.getItem(window.localStorage.key(index) ?? ""),
    )
    expect(storedValues).not.toContain("replacement-key")
  })
})
