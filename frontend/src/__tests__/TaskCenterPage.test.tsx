import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TaskCenterPage } from "@/features/tasks/TaskCenterPage"
import { renderWithProviders } from "@/test/render"

function envelope(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({ code: status, message: "success", data }),
    { status, headers: { "Content-Type": "application/json" } },
  )
}

const taskType = {
  type: "music.embed_metadata",
  payload_version: 1,
  manual: true,
  schedulable: false,
  cancelable: true,
}

const runningRun = {
  id: 41,
  type: "music.embed_metadata",
  payload_version: 1,
  subject: {
    path: "/Music/Ocean Eyes.flac",
    embed_cover: true,
    embed_lyrics: true,
  },
  trigger: "manual",
  status: "running",
  progress: { stage: "downloading", percent: 46 },
  attempt: 1,
  max_attempts: 3,
  available_at: "2026-07-22T00:00:00Z",
  started_at: "2026-07-22T00:00:01Z",
  cancel_requested: false,
  created_by: "admin",
  created_at: "2026-07-22T00:00:00Z",
  updated_at: "2026-07-22T00:00:01Z",
}

const failedRun = {
  ...runningRun,
  id: 42,
  subject: { path: "/Music/Bad Track.flac" },
  status: "failed",
  progress: { stage: "embedding", percent: 72 },
  finished_at: "2026-07-22T00:00:03Z",
  error: { code: "tag_failed", message: "Unable to write tags" },
}

const queuedRun = {
  ...runningRun,
  id: 40,
  subject: { path: "/Music/Queued Track.flac" },
  status: "queued",
  progress: { stage: "", percent: 0 },
  started_at: undefined,
}

beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe("TaskCenterPage", () => {
  it("renders runs and supports cancel and retry actions", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/tasks/types") {
          return Promise.resolve(envelope({ items: [taskType] }))
        }
        if (url.startsWith("/api/manager/tasks/runs?")) {
          return Promise.resolve(
            envelope({
              items: [runningRun, failedRun],
              total: 2,
              page: 1,
              per_page: 20,
            }),
          )
        }
        if (
          url === "/api/manager/tasks/runs/41/cancel" &&
          init?.method === "POST"
        ) {
          return Promise.resolve(
            envelope({
              ...runningRun,
              cancel_requested: true,
            }),
          )
        }
        if (
          url === "/api/manager/tasks/runs/42/retry" &&
          init?.method === "POST"
        ) {
          return Promise.resolve(
            envelope(
              {
                ...failedRun,
                id: 43,
                trigger: "retry",
                status: "queued",
                progress: { stage: "", percent: 0 },
                error: undefined,
                finished_at: undefined,
              },
              202,
            ),
          )
        }
        if (url === "/api/manager/tasks/runs/43") {
          return Promise.resolve(
            envelope({
              ...failedRun,
              id: 43,
              trigger: "retry",
              status: "queued",
              progress: { stage: "", percent: 0 },
              error: undefined,
              finished_at: undefined,
            }),
          )
        }
        return Promise.reject(new Error(`unexpected request: ${url}`))
      })

    renderWithProviders(<TaskCenterPage />)

    expect(
      await screen.findAllByText("/Music/Ocean Eyes.flac"),
    ).not.toHaveLength(0)
    expect(screen.getAllByText("Running")).not.toHaveLength(0)
    expect(screen.getAllByText("Failed")).not.toHaveLength(0)

    await userEvent.click(
      screen.getAllByRole("button", { name: "Cancel task" })[0],
    )
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/tasks/runs/41/cancel",
        expect.objectContaining({ method: "POST" }),
      ),
    )

    await userEvent.click(screen.getAllByRole("button", { name: "Retry" })[0])
    expect(await screen.findByText("Task #43")).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toHaveClass(
      "grid-rows-[auto_minmax(0,1fr)_auto]",
      "overflow-hidden",
    )
    expect(
      screen.getByRole("dialog").querySelector('[data-slot="dialog-body"]'),
    ).toHaveClass("overflow-y-auto")
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/tasks/runs/42/retry",
      expect.objectContaining({ method: "POST" }),
    )
  })

  it("deletes queued and terminal runs but not running runs", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/tasks/types") {
          return Promise.resolve(envelope({ items: [taskType] }))
        }
        if (url.startsWith("/api/manager/tasks/runs?")) {
          return Promise.resolve(
            envelope({
              items: [runningRun, queuedRun, failedRun],
              total: 3,
              page: 1,
              per_page: 20,
            }),
          )
        }
        if (
          url === "/api/manager/tasks/runs/40" &&
          init?.method === "DELETE"
        ) {
          return Promise.resolve(envelope({ deleted: true }))
        }
        return Promise.reject(new Error(`unexpected request: ${url}`))
      })

    renderWithProviders(<TaskCenterPage />)

    const runningSubject = await screen.findAllByText(
      "/Music/Ocean Eyes.flac",
    )
    const runningRow = runningSubject[0].closest("tr")
    const queuedRow = screen.getAllByText("/Music/Queued Track.flac")[0].closest(
      "tr",
    )
    const failedRow = screen.getAllByText("/Music/Bad Track.flac")[0].closest(
      "tr",
    )
    expect(runningRow).not.toBeNull()
    expect(queuedRow).not.toBeNull()
    expect(failedRow).not.toBeNull()
    expect(
      within(runningRow!).queryByRole("button", { name: "Delete task run" }),
    ).not.toBeInTheDocument()
    expect(
      within(queuedRow!).getByRole("button", { name: "Delete task run" }),
    ).toBeInTheDocument()
    expect(
      within(failedRow!).getByRole("button", { name: "Delete task run" }),
    ).toBeInTheDocument()

    await userEvent.click(
      within(queuedRow!).getByRole("button", { name: "Delete task run" }),
    )
    const confirmation = screen.getByRole("dialog")
    expect(
      within(confirmation).getByRole("heading", { name: "Delete task run" }),
    ).toBeInTheDocument()
    await userEvent.click(
      within(confirmation).getByRole("button", { name: "Delete" }),
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/tasks/runs/40",
        expect.objectContaining({ method: "DELETE" }),
      ),
    )
  })

  it.each([
    { type: "files.sync", status: "failed" },
    { type: "music.catalog_upload", status: "canceled" },
  ])("keeps $type history readable without retry controls", async ({ type, status }) => {
    const historicalRun = {
      ...failedRun,
      id: 44,
      type,
      status,
      subject: null,
      progress: { stage: "", percent: 0 },
    }
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/tasks/types") {
        return Promise.resolve(envelope({ items: [taskType] }))
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(envelope({ items: [historicalRun], total: 1, page: 1, per_page: 20 }))
      }
      if (url === "/api/manager/tasks/runs/44") {
        return Promise.resolve(envelope(historicalRun))
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    const user = userEvent.setup()

    renderWithProviders(<TaskCenterPage />)
    const subjects = await screen.findAllByText("#44")
    const row = subjects[0].closest("tr")
    const card = subjects[1].closest("article")
    expect(row).not.toBeNull()
    expect(card).not.toBeNull()
    for (const container of [row!, card!]) {
      expect(within(container).getByText(type)).toBeInTheDocument()
      expect(within(container).queryByRole("button", { name: "Retry" })).not.toBeInTheDocument()
      expect(within(container).getByRole("button", { name: "Delete task run" })).toBeInTheDocument()
    }
    expect(screen.getByRole("button", { name: "Retry failed" })).toBeDisabled()
    await user.click(screen.getByRole("checkbox", { name: "Select all tasks" }))
    expect(screen.getByRole("button", { name: "Retry selected" })).toBeDisabled()

    await user.click(within(row!).getByRole("button", { name: "View details" }))
    const dialog = await screen.findByRole("dialog")
    expect(await within(dialog).findByText(type)).toBeInTheDocument()
    expect(within(dialog).getByText("Unable to write tags")).toBeInTheDocument()
    expect(within(dialog).queryByRole("button", { name: "Retry" })).not.toBeInTheDocument()
  })

  it.each(["Retry failed", "Retry selected"])("excludes removed types from %s", async (action) => {
    const removedRuns = [
      { ...failedRun, id: 44, type: "files.sync", subject: null },
      { ...failedRun, id: 45, type: "music.catalog_upload", status: "canceled", subject: null },
    ]
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation(
      (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === "/api/manager/tasks/types") {
          return Promise.resolve(envelope({ items: [taskType] }))
        }
        if (url.startsWith("/api/manager/tasks/runs?")) {
          return Promise.resolve(envelope({ items: [failedRun, ...removedRuns], total: 3, page: 1, per_page: 20 }))
        }
        if (url === "/api/manager/tasks/runs/42/retry" && init?.method === "POST") {
          return Promise.resolve(envelope({ ...queuedRun, id: 43 }, 202))
        }
        return Promise.reject(new Error(`unexpected request: ${url}`))
      },
    )
    const user = userEvent.setup()

    renderWithProviders(<TaskCenterPage />)
    await screen.findAllByText("/Music/Bad Track.flac")
    if (action === "Retry selected") {
      await user.click(screen.getByRole("checkbox", { name: "Select all tasks" }))
    }
    const retryButton = screen.getByRole("button", { name: action })
    await waitFor(() => expect(retryButton).toBeEnabled())
    await user.click(retryButton)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/tasks/runs/42/retry",
      expect.objectContaining({ method: "POST" }),
    ))
    expect(
      fetchMock.mock.calls
        .filter(([input, init]) => String(input).endsWith("/retry") && init?.method === "POST")
        .map(([input]) => String(input)),
    ).toEqual(["/api/manager/tasks/runs/42/retry"])
  })

  it.each([
    { language: "en", progress: "Scanning: 12 folders, 34 files", view: "View details" },
    { language: "zh", progress: "正在扫描：12 个目录，34 个文件", view: "查看详情" },
  ])("renders media scan counts in $language desktop, mobile, and detail views", async ({ language, progress, view }) => {
    window.localStorage.setItem("openlist2emby.language", language)
    const scanRun = {
      ...runningRun,
      type: "media.scan",
      subject: { library_id: 1, library_name: "Movies" },
      progress: { stage: "scanning", percent: 0, directories_scanned: 12, files_scanned: 34 },
    }
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/tasks/types") {
        return Promise.resolve(envelope({ items: [{ ...taskType, type: "media.scan", schedulable: true }] }))
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(envelope({ items: [scanRun], total: 1, page: 1, per_page: 20 }))
      }
      if (url === "/api/manager/tasks/runs/41") {
        return Promise.resolve(envelope(scanRun))
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    const user = userEvent.setup()

    renderWithProviders(<TaskCenterPage />)
    const labels = await screen.findAllByText(progress)
    expect(labels).toHaveLength(2)
    expect(labels[0].closest("tr")).not.toBeNull()
    expect(labels[1].closest("article")).not.toBeNull()
    await user.click(screen.getAllByRole("button", { name: view })[0])
    expect(await within(screen.getByRole("dialog")).findByText(progress)).toBeInTheDocument()
  })

  it("does not display media scan schedules", async () => {
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/tasks/types") {
        return Promise.resolve(
          envelope({
            items: [
              taskType,
              {
                type: "media.scan",
                payload_version: 1,
                manual: true,
                schedulable: true,
                cancelable: true,
              },
            ],
          }),
        )
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(
          envelope({ items: [], total: 0, page: 1, per_page: 20 }),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<TaskCenterPage />)
    expect(await screen.findByText("No task runs")).toBeInTheDocument()
    expect(screen.queryByText("Media scan schedules")).not.toBeInTheDocument()
    expect(screen.queryByText("Movies")).not.toBeInTheDocument()
    expect(
      screen.queryByRole("tab", { name: "Schedules" }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "New schedule" }),
    ).not.toBeInTheDocument()
  })

  it("limits OpenList running task names and keeps the full name available", async () => {
    const longName =
      "/Media/Downloads/A Very Long Movie Title That Should Not Stretch The Table (2024)/A Very Long Movie Title That Should Not Stretch The Table (2024).mkv"
    vi.spyOn(window, "fetch").mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/tasks/types") {
        return Promise.resolve(envelope({ items: [taskType] }))
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(
          envelope({ items: [], total: 0, page: 1, per_page: 20 }),
        )
      }
      if (url === "/api/manager/openlist/tasks") {
        return Promise.resolve(
          envelope({
            items: [
              {
                id: "copy-1",
                kind: "copy",
                name: longName,
                state: 1,
                status: "running",
                progress: 42,
                start_time: "2026-07-22T00:00:01Z",
                total_bytes: 1024,
              },
            ],
          }),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<TaskCenterPage />)

    const name = await screen.findByTitle(longName)
    expect(name).toHaveClass("overflow-hidden", "w-72")
    expect(name.closest("td")).toHaveClass("w-72", "max-w-72")
    expect(screen.getByText("Copy")).toBeInTheDocument()
  })
})
