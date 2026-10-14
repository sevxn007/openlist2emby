import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { DirectoryTree } from "@/features/files/DirectoryTree"
import { FileListResponse, OpenListObject } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

function directory(name: string): OpenListObject {
  return {
    name,
    size: 0,
    is_dir: true,
    modified: "2026-01-02T03:04:05Z",
    created: "2026-01-02T03:04:05Z",
    sign: "",
    thumb: "",
    type: 1,
    hashinfo: "",
  }
}

function response(content: OpenListObject[] = []): FileListResponse {
  return {
    content,
    total: content.length,
    readme: "",
    header: "",
    write: false,
    write_content_bypass: false,
    provider: "unknown",
  }
}

describe("DirectoryTree", () => {
  it("loads the root and expands child folders lazily", async () => {
    const listFilesFn = vi.fn().mockImplementation(({ path }: { path: string }) => {
      if (path === "/") {
        return Promise.resolve(response([directory("Media"), directory("Archive")]))
      }
      if (path === "/Media") {
        return Promise.resolve(response([directory("Season 1")]))
      }
      return Promise.resolve(response())
    })

    renderWithProviders(
      <DirectoryTree
        isDisabled={() => false}
        listFilesFn={listFilesFn}
        selectedPath={null}
        onSelect={() => undefined}
      />,
    )

    expect(await screen.findByLabelText("Select folder Archive")).toBeInTheDocument()
    expect(screen.queryByLabelText(/Folder actions for/)).not.toBeInTheDocument()
    expect(listFilesFn).toHaveBeenCalledWith({ path: "/" })
    expect(listFilesFn).not.toHaveBeenCalledWith({ path: "/Media" })

    await userEvent.click(screen.getByLabelText("Expand folder Media"))

    expect(await screen.findByLabelText("Select folder Season 1")).toBeInTheDocument()
    expect(listFilesFn).toHaveBeenCalledWith({ path: "/Media" })
  })

  it("keeps blocked folders expandable while preventing selection", async () => {
    const onSelect = vi.fn()
    const listFilesFn = vi.fn().mockImplementation(({ path }: { path: string }) => {
      if (path === "/") {
        return Promise.resolve(response([directory("Source")]))
      }
      if (path === "/Source") {
        return Promise.resolve(response([directory("Child")]))
      }
      return Promise.resolve(response())
    })

    renderWithProviders(
      <DirectoryTree
        isDisabled={(path) => path === "/" || path.startsWith("/Source")}
        listFilesFn={listFilesFn}
        selectedPath={null}
        onSelect={onSelect}
      />,
    )

    expect(screen.getByLabelText("Select folder Root")).toBeDisabled()
    const source = await screen.findByLabelText("Select folder Source")
    expect(source).toBeDisabled()

    await userEvent.click(screen.getByLabelText("Expand folder Source"))

    expect(await screen.findByLabelText("Select folder Child")).toBeDisabled()
    expect(onSelect).not.toHaveBeenCalled()
  })

  it("marks every selected path when used as a multi-folder picker", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response([directory("Movies"), directory("Shows")]),
    )

    renderWithProviders(
      <DirectoryTree
        isDisabled={() => false}
        listFilesFn={listFilesFn}
        selectedPath={null}
        selectedPaths={["/Movies", "/Shows"]}
        onSelect={() => undefined}
      />,
    )

    expect(await screen.findByLabelText("Select folder Movies")).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    expect(screen.getByLabelText("Select folder Shows")).toHaveAttribute(
      "aria-pressed",
      "true",
    )
  })

  it("shows a retry action when a folder fails to load", async () => {
    const listFilesFn = vi
      .fn()
      .mockRejectedValueOnce(new Error("unavailable"))
      .mockResolvedValueOnce(response([directory("Recovered")]))

    renderWithProviders(
      <DirectoryTree
        isDisabled={() => false}
        listFilesFn={listFilesFn}
        selectedPath={null}
        onSelect={() => undefined}
      />,
    )

    expect(await screen.findByText("Unable to load folders")).toBeInTheDocument()
    await userEvent.click(screen.getByLabelText("Retry loading folders"))

    expect(await screen.findByLabelText("Select folder Recovered")).toBeInTheDocument()
    await waitFor(() => expect(listFilesFn).toHaveBeenCalledTimes(2))
  })
})
