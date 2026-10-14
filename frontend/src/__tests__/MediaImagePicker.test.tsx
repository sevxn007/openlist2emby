import {screen, waitFor} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest"
import {MediaImagePicker} from "@/features/media/MediaImagePicker"
import {type MediaLibraryItem} from "@/shared/api/manager"
import {renderWithProviders} from "@/test/render"

const item: MediaLibraryItem = {
    id: 1,
    library_id: 1,
    kind: "movie",
    path: "/movie",
    name: "Film",
    sort_name: "Film",
    size_bytes: 0,
    tmdb_id: 10,
    tvdb_id: 20,
    resolved_metadata_source: "tmdb",
    user_data: {playback_position_ticks: 0, played: false, play_count: 0, favorite: false},
    created_at: "2026-01-01",
    updated_at: "2026-01-01"
}
const images = [{
    source: "tmdb",
    image_type: "primary",
    url: "https://images.example/one.jpg",
    thumbnail_url: "https://images.example/thumb.jpg",
    width: 800,
    height: 1200,
    language: "zh"
}, {
    source: "tmdb",
    image_type: "primary",
    url: "https://images.example/two.jpg",
    thumbnail_url: "https://images.example/two.jpg"
}]

function envelope(data: unknown, status = 200) {
    return new Response(JSON.stringify({code: status, message: "success", data}), {
        status,
        headers: {"Content-Type": "application/json"}
    })
}

function mockFetch(empty = false) {
    return vi.spyOn(window, "fetch").mockImplementation((input) => {
        const url = String(input)
        if (url.includes("image-sources")) return Promise.resolve(envelope({sources: ["tvdb", "tmdb"]}))
        if (url.includes("/media/search")) return Promise.resolve(envelope({
            results: [{
                id: 55,
                media_type: "movie",
                title: "Different Film",
                source: "tvdb"
            }]
        }))
        if (url.includes("/items/3")) return Promise.resolve(envelope({...item, id: 3, kind: "series", tmdb_id: 88}))
        return Promise.resolve(envelope({images: empty ? [] : images}))
    })
}

beforeEach(() => window.localStorage.setItem("openlist2emby.language", "en"))
afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear()
})
describe("MediaImagePicker", () => {
    it("loads the actual source and stages a keyboard selection until confirmed", async () => {
        const fetchMock = mockFetch();
        const onSelect = vi.fn();
        const onClose = vi.fn()
        renderWithProviders(<MediaImagePicker item={item} imageType="primary" onSelect={onSelect} onClose={onClose}/>)
        const candidate = await screen.findByRole("button", {name: "Image 2"})
        expect(fetchMock.mock.calls.some(([url]) => String(url).includes("source=tmdb&type=movie&id=10&image_type=primary"))).toBe(true)
        expect(screen.getByText("800 × 1200 · zh")).toBeInTheDocument()
        candidate.focus();
        await userEvent.keyboard("{Enter}")
        expect(candidate).toHaveAttribute("aria-pressed", "true")
        expect(onSelect).not.toHaveBeenCalled()
        await userEvent.click(screen.getByRole("button", {name: "Use selected image"}))
        expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({
            image: images[1],
            query: expect.objectContaining({id: 10, source: "tmdb"})
        }))
        expect(fetchMock.mock.calls.every(([, init]) => !init?.method || init.method === "GET")).toBe(true)
    })
    it("switches source and searches another work without editing it", async () => {
        const fetchMock = mockFetch();
        const onSelect = vi.fn()
        renderWithProviders(<MediaImagePicker item={{...item, tmdb_id: undefined, tvdb_id: undefined}}
                                              imageType="primary" onSelect={onSelect} onClose={vi.fn()}/>)
        await screen.findByText("Select a title to find images")
        await userEvent.click(screen.getByRole("combobox", {name: "Metadata source"}))
        await userEvent.click(screen.getByRole("option", {name: "TVDB"}))
        await userEvent.clear(screen.getByLabelText("Movie or TV title"))
        await userEvent.type(screen.getByLabelText("Movie or TV title"), "Different Film{Enter}")
        await userEvent.click(await screen.findByRole("button", {name: "Different Film"}))
        await userEvent.click(await screen.findByRole("button", {name: "Image 1"}))
        await userEvent.click(screen.getByRole("button", {name: "Use selected image"}))
        expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({
            query: expect.objectContaining({
                id: 55,
                source: "tvdb"
            })
        }))
        expect(fetchMock.mock.calls.some(([url]) => String(url).includes("query=Different+Film"))).toBe(true)
    })
    it("uses the parent series identity for an episode", async () => {
        const fetchMock = mockFetch()
        renderWithProviders(<MediaImagePicker
            item={{...item, kind: "episode", tmdb_id: 999, series_id: 3, season_number: 0, episode_number: 2}}
            imageType="primary" onSelect={vi.fn()} onClose={vi.fn()}/>)
        await screen.findByRole("button", {name: "Image 1"})
        expect(fetchMock.mock.calls.some(([url]) => String(url).includes("id=88&image_type=primary&season=0&episode=2"))).toBe(true)
    })
    it.each(["en", "zh"])("renders empty state and cancels without selecting in %s", async (locale) => {
        window.localStorage.setItem("openlist2emby.language", locale)
        mockFetch(true);
        const onClose = vi.fn();
        const onSelect = vi.fn()
        renderWithProviders(<MediaImagePicker item={item} imageType="backdrop" onSelect={onSelect} onClose={onClose}/>)
        await screen.findByText(locale === "en" ? "No images available" : "暂无可用图片")
        await userEvent.click(screen.getByRole("button", {name: locale === "en" ? "Cancel" : "取消"}))
        expect(onClose).toHaveBeenCalled();
        expect(onSelect).not.toHaveBeenCalled()
    })
    it("retries a failed candidate request", async () => {
        let failed = true
        vi.spyOn(window, "fetch").mockImplementation((url) => Promise.resolve(String(url).includes("image-sources") ? envelope({sources: ["tmdb"]}) : envelope({images}, failed ? 502 : 200)))
        renderWithProviders(<MediaImagePicker item={item} imageType="primary" onSelect={vi.fn()} onClose={vi.fn()}/>)
        await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument())
        failed = false
        await userEvent.click(screen.getByRole("button", {name: "Retry"}))
        await screen.findByRole("button", {name: "Image 1"})
    })
})
