import {act, screen, waitFor, within} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest"

import {MetadataSettingsSection} from "@/features/settings/components/MetadataSettingsSection"
import {SettingsResponse} from "@/shared/api/manager"
import {THEME_STORAGE_KEY} from "@/shared/theme/ThemeProvider"
import {renderWithProviders} from "@/test/render"

const settings: SettingsResponse = {
  openlist_base_url: "http://openlist.local",
  openlist_admin_key_set: true,
  openlist_admin_key: "********",
  tmdb_api_key_set: true,
  tmdb_api_key: "********",
  tvdb_api_key_set: true,
  tvdb_api_key: "********",
  tvdb_pin_set: true,
  tvdb_pin: "********",
  metadata_priority: ["tmdb", "tvdb"],
  media_library_enabled: true,
  emby_port: 8096,
}

function envelope(data: unknown, status = 200, message = "success") {
  return new Response(JSON.stringify({ code: status, message, data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

beforeEach(() => window.localStorage.setItem("openlist2emby.language", "en"))
afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe("MetadataSettingsSection", () => {
  it.each([
    { locale: "en", theme: "light", title: "Movie & TV metadata", priority: "Source priority", save: "Save metadata settings" },
    { locale: "en", theme: "dark", title: "Movie & TV metadata", priority: "Source priority", save: "Save metadata settings" },
    { locale: "zh", theme: "light", title: "影视元数据", priority: "来源优先级", save: "保存元数据设置" },
    { locale: "zh", theme: "dark", title: "影视元数据", priority: "来源优先级", save: "保存元数据设置" },
  ])("uses theme tokens and responsive provider groups in $locale/$theme", ({ locale, theme, title, priority, save }) => {
    window.localStorage.setItem("openlist2emby.language", locale)
    window.localStorage.setItem(THEME_STORAGE_KEY, theme)
    renderWithProviders(<MetadataSettingsSection settings={settings} onSaved={vi.fn()} />)

    expect(document.documentElement.classList.contains("dark")).toBe(theme === "dark")
    const form = screen.getByRole("form", { name: title })
    expect(form).toHaveClass("@container/metadata-settings")
    const tmdb = within(form).getByRole("region", { name: "TMDB" })
    const tvdb = within(form).getByRole("region", { name: "TVDB" })
    expect(tmdb.parentElement).toBe(tvdb.parentElement)
    expect(tmdb.parentElement).toHaveClass("grid", "@xl/field-group:grid-cols-2", "min-w-0")
    for (const group of [tmdb, tvdb]) {
      expect(group).toHaveClass("bg-background", "border-border", "min-w-0")
      expect(within(group).getByRole("button")).toHaveClass("w-full", "h-11")
    }
    expect(screen.getByRole("combobox", { name: priority })).toHaveClass("w-full", "min-w-0")
    const footer = screen.getByRole("button", { name: save }).parentElement
    expect(footer).toHaveClass("flex-col", "@xl/metadata-settings:flex-row", "bg-muted/50")
    const inputs = form.querySelectorAll('input[type="password"]')
    expect(inputs).toHaveLength(3)
    inputs.forEach((input) => expect(input).toHaveValue(""))
  })

  it("saves TVDB credentials and priority without persisting secrets in browser storage", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(envelope(settings))
    const onSaved = vi.fn()
    renderWithProviders(<MetadataSettingsSection settings={settings} onSaved={onSaved} />)

    await userEvent.type(screen.getByLabelText("TVDB API Key"), "synthetic-tvdb-key")
    await userEvent.type(screen.getByLabelText("Subscription PIN (optional)"), "synthetic-pin")
    screen.getByRole("combobox", { name: "Source priority" }).focus()
    await userEvent.keyboard("{Enter}")
    await userEvent.keyboard("{End}{Enter}")
    await waitFor(() => expect(screen.queryByRole("listbox")).not.toBeInTheDocument())
    await userEvent.click(screen.getByRole("button", { name: "Save metadata settings" }))

    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(settings))
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toMatchObject({
      tvdb_api_key: "synthetic-tvdb-key",
      tvdb_pin: "synthetic-pin",
      metadata_priority: ["tvdb", "tmdb"],
    })
    expect(screen.getByLabelText("TVDB API Key")).toHaveValue("")
    expect(screen.getByLabelText("Subscription PIN (optional)")).toHaveValue("")
    expect(JSON.stringify(window.localStorage)).not.toContain("synthetic-")
  })

    it("preserves saved credentials when inputs are left blank", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(envelope(settings))
        const onSaved = vi.fn()
        renderWithProviders(<MetadataSettingsSection settings={settings} onSaved={onSaved}/>)

        expect(screen.queryByRole("checkbox")).not.toBeInTheDocument()
        expect(screen.getByLabelText("TVDB API Key")).toBeEnabled()
        expect(screen.getByLabelText("Subscription PIN (optional)")).toBeEnabled()
        expect(screen.getByRole("button", {name: "Test TVDB connection"})).toBeEnabled()
    await userEvent.click(screen.getByRole("button", { name: "Save metadata settings" }))
        await waitFor(() => expect(onSaved).toHaveBeenCalledWith(settings))
        const payload = JSON.parse(String(fetchMock.mock.calls[0][1]?.body))
        expect(payload).toMatchObject({tmdb_api_key: "", tvdb_api_key: "", tvdb_pin: ""})
        expect(payload).not.toHaveProperty("clear_tvdb_api_key")
        expect(payload).not.toHaveProperty("clear_tvdb_pin")
  })

  it("shows a pending TVDB test, prevents concurrent edits and keeps failure feedback inline", async () => {
    let resolveResponse: (response: Response) => void = () => { throw new Error("test not started") }
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation(() => new Promise<Response>((resolve) => {
      resolveResponse = resolve
    }))
    renderWithProviders(<MetadataSettingsSection settings={settings} onSaved={vi.fn()} />)

    await userEvent.type(screen.getByLabelText("TVDB API Key"), "synthetic-tvdb-key")
    await userEvent.click(screen.getByRole("button", { name: "Test TVDB connection" }))
    expect(screen.getByRole("button", { name: "Testing connection" })).toBeDisabled()
    expect(screen.getByLabelText("TVDB API Key")).toBeDisabled()
    expect(screen.getByRole("combobox", { name: "Source priority" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Save metadata settings" })).toBeDisabled()
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toMatchObject({
      source: "tvdb", tvdb_api_key: "synthetic-tvdb-key",
    })
    await act(async () => resolveResponse(envelope(null, 502, "Metadata service unavailable")))
    expect(await screen.findByRole("alert")).toHaveTextContent("Metadata service unavailable")
    expect(screen.getByRole("button", { name: "Test TVDB connection" })).toBeEnabled()
    expect(screen.getByLabelText("TVDB API Key")).toHaveValue("synthetic-tvdb-key")
    await userEvent.type(screen.getByLabelText("TVDB API Key"), "-retry")
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })
})
