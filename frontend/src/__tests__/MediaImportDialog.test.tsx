import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "@/features/files/FileBrowser"
import { MediaImportDialog } from "@/features/files/MediaImportDialog"
import type { FileListResponse, MediaLibrary, TaskRun } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

const library: MediaLibrary = {
  id: 7, name: "Shows", content_type: "tvshows", sort_order: 0,
  root_path: "/TV", root_paths: ["/TV", "/Archive"],
  video_screenshot_enabled: false, video_screenshot_percent: 10,
  playback_completion_percent: 90, hide_people_without_images: false,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
}
const run: TaskRun = {
  id: 81, type: "media.scan", payload_version: 1,
  subject: { library_id: 7, library_name: "Shows", paths: ["/TV/Show/Show.S01E02.mkv"] },
  trigger: "manual", status: "queued", progress: { stage: "queued", percent: 0 },
  attempt: 0, max_attempts: 3, cancel_requested: false, created_by: "admin",
  available_at: "2026-01-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
}
const episode: FileListResponse["content"][number] = {
  name: "Show.S01E02.mkv", size: 1024, is_dir: false, type: 2,
  modified: "2026-01-01T00:00:00Z", created: "2026-01-01T00:00:00Z",
  sign: "", thumb: "", hashinfo: "",
}

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: status, message: status >= 400 ? "synthetic failure" : "success", data }), {
    status, headers: { "Content-Type": "application/json" },
  })
}

beforeEach(() => {
  window.history.replaceState({}, "", "/?path=/TV/Show")
  window.localStorage.setItem("openlist2emby.language", "en")
})
afterEach(() => vi.restoreAllMocks())

describe("media import", () => {
  it("imports a selected episode from the file browser and tracks its durable task", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      if (String(input) === "/api/manager/media-libraries?include_hidden=true") return envelope({ items: [library], total: 1 })
      if (String(input) === "/api/manager/media-libraries/7/import") return envelope(run, 202)
      throw new Error(`unexpected request: ${String(input)}`)
    })
    const onTaskCreated = vi.fn()
    const listFilesFn = vi.fn().mockResolvedValue({ content: [episode], total: 1, readme: "", header: "", write: false, write_content_bypass: false, provider: "unknown" } satisfies FileListResponse)
    renderWithProviders(<FileBrowser mediaLibraryEnabled listFilesFn={listFilesFn} onTaskCreated={onTaskCreated} />)
    await userEvent.click((await screen.findAllByLabelText("Select Show.S01E02.mkv"))[0])
    await userEvent.click(screen.getByRole("button", { name: "Import media" }))
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).getByText("/TV/Show/Show.S01E02.mkv")).toBeInTheDocument()
    await waitFor(() => expect(within(dialog).getByRole("combobox", { name: "Target library" })).toHaveTextContent("Shows"))
    await userEvent.click(within(dialog).getByRole("button", { name: "Import media" }))
    await waitFor(() => expect(onTaskCreated).toHaveBeenCalledWith(run))
    expect(fetchMock).toHaveBeenCalledWith("/api/manager/media-libraries/7/import", expect.objectContaining({ method: "POST", body: JSON.stringify({ paths: ["/TV/Show/Show.S01E02.mkv"] }) }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(screen.queryByRole("toolbar")).not.toBeInTheDocument()
  })

  it("requires a library choice when a directory belongs to multiple libraries", async () => {
    vi.spyOn(window, "fetch").mockResolvedValue(envelope({ items: [library, { ...library, id: 8, name: "Second library" }], total: 2 }))
    renderWithProviders(<MediaImportDialog currentPath="/Archive" objects={[{ ...episode, name: "Show", is_dir: true }]} onClose={vi.fn()} onCreated={vi.fn()} />)
    const submit = screen.getByRole("button", { name: "Import media" })
    const select = await screen.findByRole("combobox", { name: "Target library" })
    expect(submit).toBeDisabled()
    await userEvent.click(select)
    await userEvent.click(screen.getByRole("option", { name: "Second library" }))
    expect(submit).toBeEnabled()
  })

  it("rejects prefix collisions and renders the unmatched status in Chinese", async () => {
    window.localStorage.setItem("openlist2emby.language", "zh")
    vi.spyOn(window, "fetch").mockResolvedValue(envelope({ items: [library], total: 1 }))
    renderWithProviders(<MediaImportDialog currentPath="/TV-Other" objects={[episode]} onClose={vi.fn()} onCreated={vi.fn()} />)
    expect(await screen.findByRole("alert")).toHaveTextContent("没有媒体库包含全部所选路径。")
    expect(screen.getByRole("button", { name: "入库" })).toBeDisabled()
  })

  it("keeps the selection available after an import request fails", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (input) =>
      String(input).includes("include_hidden") ? envelope({ items: [library], total: 1 }) : envelope(null, 500))
    const onCreated = vi.fn()
    renderWithProviders(<MediaImportDialog currentPath="/TV/Show" objects={[episode]} onClose={vi.fn()} onCreated={onCreated} />)
    const submit = screen.getByRole("button", { name: "Import media" })
    await waitFor(() => expect(submit).toBeEnabled())
    await userEvent.click(submit)
    expect(await screen.findByRole("alert")).toHaveTextContent("Unable to import media")
    expect(submit).toBeEnabled()
    expect(onCreated).not.toHaveBeenCalled()
  })

  it("allows retry after the library list fails", async () => {
    vi.spyOn(window, "fetch").mockResolvedValueOnce(envelope(null, 500)).mockResolvedValueOnce(envelope({ items: [library], total: 1 }))
    renderWithProviders(<MediaImportDialog currentPath="/TV" objects={[episode]} onClose={vi.fn()} onCreated={vi.fn()} />)
    await userEvent.click(await screen.findByRole("button", { name: "Retry" }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Import media" })).toBeEnabled())
  })
})
