import {screen, waitFor} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest"

import {MediaItemMetadataDialog} from "@/features/media/MediaItemMetadataDialog"
import {MediaLibraryItem} from "@/shared/api/manager"
import {THEME_STORAGE_KEY} from "@/shared/theme/ThemeProvider"
import {renderWithProviders} from "@/test/render"

const series: MediaLibraryItem = {
 id: 18, library_id: 1, kind: "series", path: "/series", name: "Show", sort_name: "Show", size_bytes: 0,
 tmdb_id: 101, tvdb_id: 72860, metadata_source: "auto", resolved_metadata_source: "tmdb",
 user_data: { playback_position_ticks: 0, played: false, play_count: 0, favorite: false },
 created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
}
function envelope(data: unknown, status = 200) { return new Response(JSON.stringify({ code: status, message: status === 200 ? "success" : "Refresh failed", data }), { status, headers: { "Content-Type": "application/json" } }) }
function setup(item = series, fail = false) {
 let saved = item
 const fetchMock = vi.spyOn(window, "fetch").mockImplementation((_input, init) => {
  if (init?.method === "PUT") { saved = { ...saved, ...JSON.parse(String(init.body)) }; return Promise.resolve(envelope(saved)) }
  return Promise.resolve(envelope(saved, fail ? 502 : 200))
 })
 const onSaved = vi.fn()
 const rendered = renderWithProviders(<MediaItemMetadataDialog item={item} open onSaved={onSaved} onClose={vi.fn()} />)
 return { fetchMock, onSaved, ...rendered }
}
beforeEach(() => window.localStorage.setItem("openlist2emby.language", "en"))
afterEach(() => {
 vi.restoreAllMocks()
 window.localStorage.clear()
})
describe("source-neutral metadata editing", () => {
 it.each(["light", "dark"])("aligns provider controls using theme tokens in %s mode", (theme) => {
  window.localStorage.setItem(THEME_STORAGE_KEY, theme)
  const { baseElement } = setup()
  expect(document.documentElement.classList.contains("dark")).toBe(theme === "dark")
  const tmdbField = screen.getByLabelText("TMDB ID").parentElement
  const tvdbField = screen.getByLabelText("TVDB ID").parentElement
  const providerGroup = tmdbField?.parentElement
  expect(tvdbField?.parentElement).toBe(providerGroup)
  expect(providerGroup).toHaveClass("bg-muted/20", "border-border", "min-w-0", "sm:grid-cols-2")
  const source = screen.getByRole("combobox", { name: "Metadata source" })
  const search = screen.getByRole("button", { name: "Search metadata" })
  expect(source).toHaveClass("data-[size=default]:h-11", "w-full", "min-w-0")
  expect(source).toHaveAttribute("data-size", "default")
  expect(search).toHaveClass("h-11", "w-full", "sm:w-auto")
  expect(search.parentElement).toHaveClass("items-end", "sm:grid-cols-[minmax(0,1fr)_auto]")
  expect(baseElement.querySelector('[data-slot="dialog-header"]')).toHaveClass("pr-10")
  expect(baseElement.querySelector('[data-slot="dialog-body"]')).toHaveClass("min-w-0")
 })

 it("preserves two provider IDs without sending historical bindings", async () => {
  const { fetchMock, onSaved } = setup()
  expect(screen.getByLabelText("TMDB ID")).toHaveValue(101)
  expect(screen.getByLabelText("TVDB ID")).toHaveValue(72860)
  expect(screen.queryByRole("switch")).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Save metadata" }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  const payload = JSON.parse(String(fetchMock.mock.calls[0][1]?.body))
  expect(payload).toMatchObject({ tmdb_id: 101, tvdb_id: 72860, metadata_source: "auto" })
  expect(payload).not.toHaveProperty("metadata_binding")
  expect(fetchMock).toHaveBeenCalledTimes(1)
 })
 it("fixes the selected provider and refreshes through the common endpoint", async () => {
  const { fetchMock, onSaved } = setup()
  await userEvent.click(screen.getByLabelText("Metadata source"))
  await userEvent.click(screen.getByRole("option", { name: "TVDB" }))
  await userEvent.click(screen.getByRole("button", { name: "Save metadata" }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({ metadata_source: "tvdb" })
 })
 it("retains the unfinished refresh when retrying after failure", async () => {
  const { fetchMock, onSaved } = setup(series, true)
  await userEvent.clear(screen.getByLabelText("TVDB ID"))
  await userEvent.type(screen.getByLabelText("TVDB ID"), "12345")
  await userEvent.click(screen.getByRole("button", { name: "Save metadata" }))
  await screen.findByText("Refresh failed")
  await userEvent.click(screen.getByRole("button", { name: "Save metadata" }))
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
  expect(onSaved).not.toHaveBeenCalled()
 })
 it.each(["en", "zh"])("renders translated source fields in %s", (locale) => {
  window.localStorage.setItem("openlist2emby.language", locale)
  setup({ ...series, kind: "episode", season_number: 1940, episode_number: 1 })
  expect(screen.getByLabelText("TVDB ID")).toHaveValue(72860)
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument()
 })
})

describe("remote image editing", () => {
    function remoteSetup() {
        const calls: { url: string; method?: string; body?: unknown }[] = []
        const candidate = {
            source: "tmdb",
            image_type: "primary",
            url: "https://images.example/chosen.jpg",
            thumbnail_url: "https://images.example/preview.jpg"
        }
        const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input, init) => {
            const url = String(input);
            calls.push({
                url,
                method: init?.method,
                body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined
            })
            if (url.includes("image-sources")) return Promise.resolve(envelope({sources: ["tmdb", "tvdb"]}))
            if (url.includes("/media/images?")) return Promise.resolve(envelope({images: [candidate]}))
            return Promise.resolve(envelope({...series, image_url: "/api/local-image"}))
        })
        const onSaved = vi.fn()
        renderWithProviders(<MediaItemMetadataDialog item={series} open onSaved={onSaved} onClose={vi.fn()}/>)
        return {calls, fetchMock, onSaved}
    }

    async function selectPoster() {
        await userEvent.click(screen.getAllByRole("button", {name: "Search images"})[0])
        await userEvent.click(await screen.findByRole("button", {name: "Image 1"}))
        await userEvent.click(screen.getByRole("button", {name: "Use selected image"}))
    }

    it("applies selected artwork after a source refresh without changing metadata IDs", async () => {
        const {calls, onSaved} = remoteSetup()
        await selectPoster()
        expect(calls.every((call) => !call.method || call.method === "GET")).toBe(true)
        await userEvent.click(screen.getByLabelText("Metadata source"))
        await userEvent.click(screen.getByRole("option", {name: "TVDB"}))
        await userEvent.click(screen.getByRole("button", {name: "Save metadata"}))
        await waitFor(() => expect(onSaved).toHaveBeenCalled())
        const writes = calls.filter((call) => call.method === "PUT" || call.method === "POST")
        expect(writes.map((call) => call.url)).toEqual(["/api/manager/media-library/items/18/metadata", "/api/manager/media-library/items/18/metadata/refresh", "/api/manager/media-library/items/18/images/primary/select"])
        expect(writes[0].body).toMatchObject({tmdb_id: 101, tvdb_id: 72860})
        expect(writes[2].body).toMatchObject({source: "tmdb", id: 101, url: "https://images.example/chosen.jpg"})
    })
    it("uses the last local upload instead of the remote choice", async () => {
        const {calls, onSaved} = remoteSetup()
        await selectPoster()
        const input = document.querySelector<HTMLInputElement>('#media-metadata-primary-image')!
        await userEvent.upload(input, new File(["synthetic-image"], "poster.png", {type: "image/png"}))
        await userEvent.click(screen.getByRole("button", {name: "Save metadata"}))
        await waitFor(() => expect(onSaved).toHaveBeenCalled())
        expect(calls.some((call) => call.url.endsWith("/select"))).toBe(false)
        expect(calls.some((call) => call.url.endsWith("/images/primary") && call.method === "PUT")).toBe(true)
    })
    it("uses a remote choice made after a local upload", async () => {
        const {calls, onSaved} = remoteSetup()
        const input = document.querySelector<HTMLInputElement>('#media-metadata-primary-image')!
        await userEvent.upload(input, new File(["synthetic-image"], "poster.png", {type: "image/png"}))
        await selectPoster()
        await userEvent.click(screen.getByRole("button", {name: "Save metadata"}))
        await waitFor(() => expect(onSaved).toHaveBeenCalled())
        expect(calls.some((call) => call.url.endsWith("/select"))).toBe(true)
        expect(calls.some((call) => call.url.endsWith("/images/primary") && call.method === "PUT")).toBe(false)
    })
    it("preserves a staged choice when the image picker is cancelled", async () => {
        const {calls, onSaved} = remoteSetup()
        await selectPoster()
        await userEvent.click(screen.getAllByRole("button", {name: "Search images"})[0])
        await screen.findByRole("button", {name: "Image 1"})
        await userEvent.click(screen.getByRole("button", {name: "Cancel"}))
        await userEvent.click(screen.getByRole("button", {name: "Save metadata"}))
        await waitFor(() => expect(onSaved).toHaveBeenCalled())
        expect(calls.filter((call) => call.url.endsWith("/select"))).toHaveLength(1)
    })
})
