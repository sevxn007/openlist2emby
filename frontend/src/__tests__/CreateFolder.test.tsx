import { fireEvent, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { CreateFolderButton } from "@/features/files/CreateFolderButton"
import { TransferDialog } from "@/features/files/BulkActions"
import { FileBrowser } from "@/features/files/FileBrowser"
import { type FileListResponse, type OpenListObject } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

function directory(name: string): OpenListObject {
  return { name, size: 0, is_dir: true, modified: "2026-01-01", created: "2026-01-01", sign: "", thumb: "", type: 1, hashinfo: "" }
}
function listing(content: OpenListObject[] = []): FileListResponse {
  return { content, total: content.length, readme: "", header: "", write: false, write_content_bypass: false, provider: "unknown" }
}
function envelope(code = 200, message = "success") {
  return new Response(JSON.stringify({ code, message, data: null }), { status: 200 })
}
const fetchMock = vi.fn()
beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
  window.history.replaceState({}, "", "/")
  vi.stubGlobal("fetch", fetchMock)
  fetchMock.mockReset().mockImplementation(() => Promise.resolve(envelope()))
})
afterEach(() => vi.unstubAllGlobals())

async function openCreation() {
  await userEvent.click(screen.getByRole("button", { name: "New folder" }))
  return screen.getByRole("dialog", { name: "New folder" })
}
async function createNamed(name: string) {
  const dialog = await openCreation()
  await userEvent.type(within(dialog).getByLabelText("Folder name"), name)
  await userEvent.click(within(dialog).getByRole("button", { name: "New folder" }))
}

async function openNodeCreation(name: string, entry = "menu") {
  const button = await screen.findByLabelText(`Folder actions for ${name}`)
  if (entry === "context") {
    fireEvent.contextMenu(screen.getByLabelText(`Select folder ${name}`))
  } else {
    await userEvent.click(button)
  }
  await userEvent.click(await screen.findByRole("menuitem", { name: "New folder" }))
  return screen.getByRole("dialog", { name: "New folder" })
}

async function submitNodeCreation() {
  const dialog = screen.getByRole("dialog", { name: "New folder" })
  await userEvent.type(within(dialog).getByLabelText("Folder name"), "New")
  await userEvent.click(within(dialog).getByRole("button", { name: "New folder" }))
}

describe("Create folder", () => {
  it("creates in an empty file browser and refreshes without navigating", async () => {
    const listFilesFn = vi.fn().mockResolvedValueOnce(listing()).mockResolvedValue(listing([directory("New")]))
    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await waitFor(() => expect(screen.getByRole("button", { name: "New folder" })).toBeEnabled())
    await createNamed("  New  ")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledWith("/api/manager/openlist/fs/mkdir", expect.objectContaining({ body: JSON.stringify({ path: "/New" }) }))
    expect(listFilesFn).toHaveBeenLastCalledWith({ path: "/", refresh: true })
    expect(screen.getAllByText("New").length).toBeGreaterThan(0)
    expect(window.location.search).toBe("")
  })

  it.each(["", "   ", ".", "..", "a/b", "a\\b", "a\u0001b", "a\u007fb"])("rejects invalid name %j", async (name) => {
    renderWithProviders(<CreateFolderButton parentPath="/" />)
    const dialog = await openCreation()
    fireEvent.change(within(dialog).getByLabelText("Folder name"), { target: { value: name } })
    expect(within(dialog).getByRole("button", { name: "New folder" })).toBeDisabled()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("cancels, restores focus and clears input on reopening", async () => {
    renderWithProviders(<CreateFolderButton parentPath="/" />)
    const trigger = screen.getByRole("button", { name: "New folder" })
    const dialog = await openCreation()
    await userEvent.type(within(dialog).getByLabelText("Folder name"), "Unused")
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(trigger).toHaveFocus())
    await openCreation()
    expect(screen.getByLabelText("Folder name")).toHaveValue("")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("preserves the name after a creation error and retries", async () => {
    fetchMock.mockResolvedValueOnce(envelope(500, "Creation failed"))
    renderWithProviders(<CreateFolderButton parentPath="/Media" listFilesFn={vi.fn().mockResolvedValue(listing())} />)
    await createNamed("New")
    expect(await screen.findByRole("alert")).toHaveTextContent("Creation failed")
    expect(screen.getByLabelText("Folder name")).toHaveValue("New")
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "New folder" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("retries only the listing when creation succeeded but refresh failed", async () => {
    const onCreated = vi.fn()
    const listFilesFn = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValue(listing([directory("New")]))
    renderWithProviders(<CreateFolderButton parentPath="/Media" listFilesFn={listFilesFn} onCreated={onCreated} />)
    await createNamed("New")
    expect(await screen.findByRole("alert")).toHaveTextContent("Folder created")
    expect(screen.getByLabelText("Folder name")).toBeDisabled()
    await userEvent.click(screen.getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith("/Media/New"))
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(listFilesFn).toHaveBeenCalledTimes(2)
  })

  it("prevents duplicate submissions and closing while pending", async () => {
    let resolve!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise<Response>((done) => { resolve = done }))
    renderWithProviders(<CreateFolderButton parentPath="/" listFilesFn={vi.fn().mockResolvedValue(listing())} />)
    await createNamed("New")
    const dialog = screen.getByRole("dialog")
    expect(within(dialog).getByRole("button", { name: "New folder" })).toBeDisabled()
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled()
    await userEvent.keyboard("{Escape}")
    expect(dialog).toBeInTheDocument()
    resolve(envelope())
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it("provides Chinese labels", async () => {
    window.localStorage.setItem("openlist2emby.language", "zh")
    renderWithProviders(<CreateFolderButton parentPath="/" />)
    await userEvent.click(screen.getByRole("button", { name: "新建文件夹" }))
    expect(screen.getByLabelText("文件夹名称")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "取消" })).toBeInTheDocument()
  })

  it.each([
    ["move", "menu"], ["move", "context"], ["copy", "menu"], ["copy", "context"],
  ] as const)("selects and reveals a created destination for %s via %s without submitting", async (kind, entry) => {
    const onSubmit = vi.fn()
    let refreshed = false
    const listFilesFn = vi.fn().mockImplementation(({ path, refresh }: { path: string; refresh?: boolean }) => {
      if (refresh) refreshed = true
      return Promise.resolve(path === "/" ? listing([directory("Media")]) : path === "/Media" ? listing(refreshed ? [directory("Source"), directory("New")] : [directory("Source")]) : path === "/Media/Source" ? listing([directory("Child")]) : listing())
    })
    renderWithProviders(<TransferDialog count={1} currentPath="/Media" error={null} isPending={false} kind={kind} listFilesFn={listFilesFn} objects={[directory("Source")]} open onClose={() => undefined} onSubmit={onSubmit} />)
    expect(screen.queryByRole("button", { name: "New folder" })).not.toBeInTheDocument()
    const transferButton = screen.getByRole("button", { name: kind === "move" ? "Move items" : "Copy items" })
    expect(transferButton).toBeDisabled()
    await userEvent.click(await screen.findByLabelText("Select folder Media"))
    expect(transferButton).toBeDisabled()
    await userEvent.click(screen.getByLabelText("Select folder Root"))
    await openNodeCreation("Media", entry)
    expect(screen.getByText("/Media")).toBeInTheDocument()
    await submitNodeCreation()
    expect(await screen.findByLabelText("Select folder New")).toHaveAttribute("aria-pressed", "true")
    await waitFor(() => expect(screen.getByLabelText("Folder actions for Media")).toHaveFocus())
    expect(screen.getByLabelText("Select folder Media")).toBeEnabled()
    expect(screen.getByLabelText("Select folder Source")).toBeDisabled()
    expect(screen.queryByLabelText("Folder actions for Source")).not.toBeInTheDocument()
    await userEvent.click(screen.getByLabelText("Expand folder Source"))
    expect(await screen.findByLabelText("Select folder Child")).toBeDisabled()
    expect(screen.queryByLabelText("Folder actions for Child")).not.toBeInTheDocument()
    expect(onSubmit).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole("button", { name: kind === "move" ? "Move items" : "Copy items" }))
    expect(onSubmit).toHaveBeenCalledWith("/Media/New")
  })

  it("creates under the selected parent", async () => {
    let refreshed = false
    const listFilesFn = vi.fn().mockImplementation(({ path, refresh }: { path: string; refresh?: boolean }) => {
      if (refresh) refreshed = true
      return Promise.resolve(path === "/" ? listing([directory("Archive")]) : path === "/Archive" && refreshed ? listing([directory("New")]) : listing())
    })
    renderWithProviders(<TransferDialog count={1} currentPath="/Media" error={null} isPending={false} kind="copy" listFilesFn={listFilesFn} objects={[]} open onClose={() => undefined} onSubmit={() => undefined} />)
    await userEvent.click(await screen.findByLabelText("Select folder Archive"))
    await openNodeCreation("Archive")
    await submitNodeCreation()
    expect(await screen.findByLabelText("Select folder New")).toHaveAttribute("aria-pressed", "true")
    expect(fetchMock).toHaveBeenCalledWith("/api/manager/openlist/fs/mkdir", expect.objectContaining({ body: JSON.stringify({ path: "/Archive/New" }) }))
  })

  it("preserves the selected destination on failure and clears creation state when reopened", async () => {
    fetchMock.mockResolvedValueOnce(envelope(500, "Creation failed"))
    const listFilesFn = vi.fn().mockResolvedValue(listing([directory("Archive")]))
    const onSubmit = vi.fn()
    const props = {
      count: 1,
      currentPath: "/Media",
      error: null,
      isPending: false,
      kind: "move" as const,
      listFilesFn,
      objects: [],
      onClose: () => undefined,
      onSubmit,
    }
    renderWithProviders(<TransferDialog {...props} open />)
    await userEvent.click(await screen.findByLabelText("Select folder Archive"))
    await openNodeCreation("Root")
    await submitNodeCreation()
    expect(await screen.findByRole("alert")).toHaveTextContent("Creation failed")
    await userEvent.click(within(screen.getByRole("dialog", { name: "New folder" })).getByRole("button", { name: "Cancel" }))
    expect(screen.getByLabelText("Select folder Archive")).toHaveAttribute("aria-pressed", "true")
    await waitFor(() => expect(screen.getByLabelText("Folder actions for Root")).toHaveFocus())
    await openNodeCreation("Root")
    expect(screen.getByLabelText("Folder name")).toHaveValue("")
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    await userEvent.click(within(screen.getByRole("dialog", { name: "New folder" })).getByRole("button", { name: "Cancel" }))
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("opens a directory menu with the keyboard and restores focus on cancellation", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(listing())
    renderWithProviders(<TransferDialog count={1} currentPath="/" error={null} isPending={false} kind="copy" listFilesFn={listFilesFn} objects={[]} open onClose={() => undefined} onSubmit={() => undefined} />)
    const button = screen.getByLabelText("Folder actions for Root")
    button.focus()
    await userEvent.keyboard("{Enter}")
    const item = await screen.findByRole("menuitem", { name: "New folder" })
    await waitFor(() => expect(item).toHaveFocus())
    await userEvent.keyboard("{Enter}")
    expect(screen.getByLabelText("Folder name")).toHaveFocus()
    await userEvent.keyboard("{Escape}")
    await waitFor(() => expect(button).toHaveFocus())
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByLabelText("Select folder Root")).toHaveAttribute("aria-pressed", "false")
  })

  it("translates directory actions into Chinese", async () => {
    window.localStorage.setItem("openlist2emby.language", "zh")
    renderWithProviders(<TransferDialog count={1} currentPath="/" error={null} isPending={false} kind="move" listFilesFn={vi.fn().mockResolvedValue(listing())} objects={[]} open onClose={() => undefined} onSubmit={() => undefined} />)
    await userEvent.click(screen.getByRole("button", { name: /的文件夹操作$/ }))
    expect(await screen.findByRole("menuitem", { name: "新建文件夹" })).toBeInTheDocument()
  })

  it("retries only refresh from a node menu and blocks transfer while creating", async () => {
    let refreshes = 0
    const listFilesFn = vi.fn().mockImplementation(({ refresh }: { refresh?: boolean }) => {
      if (refresh && ++refreshes === 1) return Promise.reject(new Error("Refresh failed"))
      return Promise.resolve(listing(refreshes ? [directory("New")] : []))
    })
    const onSubmit = vi.fn()
    renderWithProviders(<TransferDialog count={1} currentPath="/Media" error={null} isPending={false} kind="move" listFilesFn={listFilesFn} objects={[]} open onClose={() => undefined} onSubmit={onSubmit} />)
    await userEvent.click(screen.getByLabelText("Select folder Root"))
    const transfer = screen.getByRole("button", { name: "Move items" })
    await openNodeCreation("Root")
    expect(transfer).toBeDisabled()
    await submitNodeCreation()
    expect(await screen.findByRole("alert")).toHaveTextContent("Folder created, but the listing could not be refreshed")
    expect(fetchMock).toHaveBeenCalledOnce()
    const dialog = screen.getByRole("dialog", { name: "New folder" })
    await userEvent.click(within(dialog).getByRole("button", { name: /retry/i }))
    expect(await screen.findByLabelText("Select folder New")).toHaveAttribute("aria-pressed", "true")
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(onSubmit).not.toHaveBeenCalled()
  })

})
