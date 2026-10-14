import { useState } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"

import { TaskRunsPanel } from "@/features/tasks/TaskRunsPanel"
import { taskQueryKeys } from "@/features/tasks/queryKeys"
import type { TaskRun, TaskRunStatus } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

const taskType = {
  type: "music.embed_metadata",
  payload_version: 1,
  manual: true,
  schedulable: false,
  cancelable: true,
}

function run(id: number, status: TaskRunStatus): TaskRun {
  return {
    id, type: taskType.type, payload_version: 1, status,
    subject: { path: `/Music/Track ${id}.flac` }, trigger: "manual",
    progress: { stage: "", percent: 0 }, attempt: 1, max_attempts: 3,
    available_at: "2026-07-22T00:00:00Z", cancel_requested: false,
    created_by: "admin", created_at: "2026-07-22T00:00:00Z", updated_at: "2026-07-22T00:00:00Z",
  }
}

function envelope(data: unknown, status = 200, message = "success") {
  return new Response(JSON.stringify({ code: status, message, data }), {
    status, headers: { "Content-Type": "application/json" },
  })
}

beforeEach(() => window.localStorage.setItem("openlist2emby.language", "en"))
afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe("TaskRunsPanel clear completed", () => {
  it.each([
    { language: "en", clear: "Clear completed", status: "Filter by status", failed: "Failed", type: "Filter by task type", music: "Music metadata", next: "Next page", success: "Cleared 25 completed task runs" },
    { language: "zh", clear: "清空已完成", status: "按状态筛选", failed: "已失败", type: "按任务类型筛选", music: "音乐元数据", next: "下一页", success: "已清空 25 个已完成任务" },
  ])("clears globally from a filtered second page in $language", async (labels) => {
    window.localStorage.setItem("openlist2emby.language", labels.language)
    const success = vi.spyOn(toast, "success")
    let cleared = false
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input, init) => {
      const url = String(input)
      if (url === "/api/manager/tasks/runs/completed" && init?.method === "DELETE") {
        cleared = true
        return Promise.resolve(envelope({ deleted: 25 }))
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        const params = new URL(url, "http://localhost").searchParams
        return Promise.resolve(envelope({ items: [run(42, "failed")], total: cleared ? 1 : 21, page: Number(params.get("page")), per_page: 20 }))
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    renderWithProviders(<TaskRunsPanel selectedRunId={null} types={[taskType]} onSelectedRunChange={vi.fn()} />)
    const user = userEvent.setup()
    await screen.findAllByText("/Music/Track 42.flac")
    await user.click(screen.getByRole("combobox", { name: labels.status }))
    await user.click(screen.getByRole("option", { name: labels.failed }))
    await user.click(screen.getByRole("combobox", { name: labels.type }))
    await user.click(screen.getByRole("option", { name: labels.music }))
    await user.click(screen.getByRole("button", { name: labels.next }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "/api/manager/tasks/runs?status=failed&type=music.embed_metadata&page=2&per_page=20",
      expect.anything(),
    ))
    expect(screen.getByRole("button", { name: labels.clear })).toBeEnabled()
    await user.click(screen.getByRole("button", { name: labels.clear }))
    await waitFor(() => expect(success).toHaveBeenCalledWith(labels.success))
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE").map(([input]) => String(input))).toEqual([
      "/api/manager/tasks/runs/completed",
    ])
    await waitFor(() => expect(screen.getByRole("button", { name: labels.next })).toBeDisabled())
    expect(screen.getAllByText("/Music/Track 42.flac")).toHaveLength(2)
  })

  it("disables duplicate requests and shows failures without reporting success", async () => {
    const success = vi.spyOn(toast, "success")
    let resolveDelete: (response: Response) => void = () => { throw new Error("delete request not started") }
    const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input, init) => {
      if (String(input) === "/api/manager/tasks/runs/completed" && init?.method === "DELETE") {
        return new Promise<Response>((resolve) => { resolveDelete = resolve })
      }
      return Promise.resolve(envelope({ items: [], total: 0, page: 1, per_page: 20 }))
    })
    renderWithProviders(<TaskRunsPanel selectedRunId={null} types={[taskType]} onSelectedRunChange={vi.fn()} />)
    const user = userEvent.setup()
    const button = screen.getByRole("button", { name: "Clear completed" })
    await user.click(button)
    expect(button).toBeDisabled()
    expect(button.querySelector('[data-slot="spinner"]')).toBeInTheDocument()
    resolveDelete(envelope(null, 500, "Synthetic cleanup failure"))
    expect(await screen.findByText("Synthetic cleanup failure")).toBeInTheDocument()
    expect(success).not.toHaveBeenCalled()
    expect(button).toBeEnabled()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(1)
  })

  it.each(["succeeded", "canceled"] as const)("removes cached %s details and item pages after clearing", async (status) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    const cachedRun = run(41, status)
    client.setQueryData(taskQueryKeys.run(41), cachedRun)
    client.setQueryData(taskQueryKeys.runItems(41), { items: [] })
    client.setQueryData(taskQueryKeys.run(43), run(43, status))
    const selectedChanged = vi.fn()
    vi.spyOn(window, "fetch").mockImplementation((input, init) => {
      const url = String(input)
      if (url === "/api/manager/tasks/runs/completed" && init?.method === "DELETE") {
        return Promise.resolve(envelope({ deleted: 2 }))
      }
      if (url === "/api/manager/tasks/runs/41") return Promise.resolve(envelope(cachedRun))
      if (url.startsWith("/api/manager/tasks/runs?")) return Promise.resolve(envelope({ items: [], total: 0, page: 1, per_page: 20 }))
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    function Panel() {
      const [selectedRunId, setSelectedRunId] = useState<number | null>(41)
      return <TaskRunsPanel selectedRunId={selectedRunId} types={[taskType]} onSelectedRunChange={(id) => {
        selectedChanged(id)
        setSelectedRunId(id)
      }} />
    }
    renderWithProviders(
      <QueryClientProvider client={client}>
        <Panel />
      </QueryClientProvider>,
    )
    // Close the modal so the toolbar can be reached by keyboard and pointer.
    await userEvent.keyboard("{Escape}")
    await userEvent.click(screen.getByRole("button", { name: "Clear completed" }))
    await waitFor(() => expect(client.getQueryData(taskQueryKeys.run(43))).toBeUndefined())
    expect(client.getQueryData(taskQueryKeys.run(41))).toBeUndefined()
    expect(client.getQueryData(taskQueryKeys.runItems(41))).toBeUndefined()
    expect(selectedChanged).toHaveBeenCalledWith(null)
    client.clear()
  })
})
