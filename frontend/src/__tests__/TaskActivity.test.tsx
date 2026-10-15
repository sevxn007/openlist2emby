import { act, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useTaskActivity } from "@/features/tasks/useTaskActivity"
import type { TaskRun } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}))

vi.mock("sonner", () => ({ toast: toastMocks }))

function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200, message: "success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })
}

const runningRun: TaskRun = {
  id: 41,
  type: "music.embed_metadata",
  payload_version: 1,
  subject: { path: "/Music/Ocean.Eyes.flac" },
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

function Harness({ run = runningRun }: { run?: TaskRun }) {
  const { activeTaskCount, trackTaskRun } = useTaskActivity(true)
  return (
    <>
      <button type="button" onClick={() => trackTaskRun(run)}>
        Track
      </button>
      <span data-testid="active-count">{activeTaskCount}</span>
    </>
  )
}

beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  vi.restoreAllMocks()
  toastMocks.error.mockReset()
  toastMocks.success.mockReset()
  window.localStorage.clear()
})

describe("task activity", () => {
  it.each(["succeeded", "failed"] as const)("notifies when a media import has %s", async (status) => {
    const imported: TaskRun = {
      ...runningRun, type: "media.scan", status,
      subject: { library_id: 7, library_name: "Shows", paths: ["/TV/Show"] },
    }
    vi.spyOn(window, "fetch").mockImplementation(async (input) =>
      String(input) === "/api/manager/tasks/runs/41" ? envelope(imported) : envelope({ items: [], total: 0 }))
    renderWithProviders(<Harness run={imported} />)
    await userEvent.click(screen.getByRole("button", { name: "Track" }))
    await waitFor(() => expect(status === "succeeded" ? toastMocks.success : toastMocks.error)
      .toHaveBeenCalledWith(status === "succeeded" ? "Media import completed" : "Unable to import media"))
  })

  it("keeps tracking a created music task and notifies when it finishes", async () => {
    let activeListRequests = 0
    let detailRequests = 0
    vi.spyOn(window, "fetch").mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith("/api/manager/tasks/runs?")) {
        activeListRequests += 1
        const active = activeListRequests === 2
        return Promise.resolve(
          envelope({
            items: active ? [runningRun] : [],
            total: active ? 1 : 0,
            page: 1,
            per_page: 1,
          }),
        )
      }
      if (url === "/api/manager/tasks/runs/41") {
        detailRequests += 1
        return Promise.resolve(
          envelope(
            detailRequests === 1
              ? runningRun
              : {
                  ...runningRun,
                  status: "succeeded",
                  progress: { stage: "completed", percent: 100 },
                  finished_at: "2026-07-22T00:00:04Z",
                  updated_at: "2026-07-22T00:00:04Z",
                  result: { embedded_cover: true, embedded_lyrics: true },
                },
          ),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<Harness />)
    await waitFor(() => expect(screen.getByTestId("active-count")).toHaveTextContent("0"))

    await userEvent.click(screen.getByRole("button", { name: "Track" }))

    await waitFor(
      () => expect(toastMocks.success).toHaveBeenCalledWith("Music metadata embedded"),
      { timeout: 2_500 },
    )
    expect(detailRequests).toBeGreaterThanOrEqual(2)
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 100))
    })
    const requestsAfterCompletion = detailRequests
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 1_100))
    })

    expect(detailRequests).toBe(requestsAfterCompletion)
    expect(toastMocks.success).toHaveBeenCalledTimes(1)
    expect(toastMocks.error).not.toHaveBeenCalled()
  })
})
