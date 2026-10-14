import { fireEvent, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "@/features/files/FileBrowser"
import { FileListResponse, ManagerApiError } from "@/shared/api/manager"
import { renderWithProviders } from "@/test/render"

function response(overrides: Partial<FileListResponse> = {}): FileListResponse {
  return {
    content: [],
    total: 0,
    readme: "",
    header: "",
    write: false,
    write_content_bypass: false,
    provider: "unknown",
    ...overrides,
  }
}

function item(
  name: string,
  overrides: Partial<FileListResponse["content"][number]> = {},
): FileListResponse["content"][number] {
  return {
    name,
    size: 1024,
    is_dir: false,
    modified: "2026-01-02T03:04:05Z",
    created: "2026-01-02T03:04:05Z",
    sign: "",
    thumb: "",
    type: 2,
    hashinfo: "",
    ...overrides,
  }
}

function desktopFileKeys() {
  const table = screen.getByRole("table")
  return Array.from(table.querySelectorAll<HTMLElement>("[data-selection-key]")).map(
    (row) => row.dataset.selectionKey,
  )
}

beforeEach(() => {
  window.history.replaceState({}, "", "/")
  window.localStorage.setItem("openlist2emby.language", "en")
})

describe("FileBrowser", () => {
  it("renders folders and files", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 2,
        content: [
          {
            name: "Movies",
            size: 0,
            is_dir: true,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "",
            thumb: "",
            type: 1,
            hashinfo: "",
          },
          {
            name: "readme.txt",
            size: 2048,
            is_dir: false,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "signed",
            thumb: "",
            type: 2,
            hashinfo: "",
          },
        ],
      }),
    )

    renderWithProviders(
      <FileBrowser
        openListBaseURL="http://openlist.local"
        listFilesFn={listFilesFn}
      />,
    )

    expect((await screen.findAllByText("Movies")).length).toBeGreaterThan(0)
    expect(screen.getAllByText("readme.txt").length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Download readme.txt")).toHaveAttribute(
      "href",
      "http://openlist.local/d/readme.txt?sign=signed",
    )
    expect(screen.getByLabelText("Rename readme.txt")).toBeInTheDocument()
    expect(screen.getByLabelText("Move readme.txt")).toBeInTheDocument()
    expect(screen.getByLabelText("Copy readme.txt")).toBeInTheDocument()
    expect(screen.getByLabelText("Delete readme.txt")).toBeInTheDocument()
    expect(
      Array.from(
        screen
          .getByLabelText("Actions for readme.txt")
          .querySelectorAll<HTMLElement>("button, a"),
      ).map((action) => action.getAttribute("aria-label")),
    ).toEqual([
      "Rename readme.txt",
      "Copy readme.txt",
      "Move readme.txt",
      "Download readme.txt",
      "Delete readme.txt",
    ])
    expect(screen.queryByText("Provider: unknown")).not.toBeInTheDocument()
  })

  it("sorts the current directory by name, size, and modified time", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [
          item("Zulu.txt", { size: 128, modified: "2026-01-01T03:04:05Z" }),
          item("Alpha.txt", { size: 4096, modified: "2026-01-03T03:04:05Z" }),
          item("Middle.txt", { size: 1024, modified: "2026-01-02T03:04:05Z" }),
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await screen.findAllByText("Alpha.txt")

    expect(desktopFileKeys()).toEqual(["f-Alpha.txt", "f-Middle.txt", "f-Zulu.txt"])

    await userEvent.click(screen.getByRole("button", { name: "Sort by Name" }))
    expect(desktopFileKeys()).toEqual(["f-Zulu.txt", "f-Middle.txt", "f-Alpha.txt"])

    await userEvent.click(screen.getByRole("button", { name: "Sort by Size" }))
    expect(desktopFileKeys()).toEqual(["f-Zulu.txt", "f-Middle.txt", "f-Alpha.txt"])

    await userEvent.click(screen.getByRole("button", { name: "Sort by Size" }))
    expect(desktopFileKeys()).toEqual(["f-Alpha.txt", "f-Middle.txt", "f-Zulu.txt"])

    await userEvent.click(screen.getByRole("button", { name: "Sort by Modified" }))
    expect(desktopFileKeys()).toEqual(["f-Alpha.txt", "f-Middle.txt", "f-Zulu.txt"])

    await userEvent.click(screen.getByRole("button", { name: "Sort by Modified" }))
    expect(desktopFileKeys()).toEqual(["f-Zulu.txt", "f-Middle.txt", "f-Alpha.txt"])
    expect(listFilesFn).toHaveBeenCalledTimes(1)
  })

  it("changes sorting from the compact sort menu", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [
          item("Zulu.txt", { size: 128 }),
          item("Alpha.txt", { size: 4096 }),
          item("Middle.txt", { size: 1024 }),
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await screen.findAllByText("Alpha.txt")

    await userEvent.click(screen.getByRole("button", { name: "Sort files" }))
    await userEvent.click(
      screen.getByRole("menuitemradio", { name: "Size: smallest first" }),
    )

    expect(desktopFileKeys()).toEqual(["f-Zulu.txt", "f-Middle.txt", "f-Alpha.txt"])
  })

  it("updates the path when opening a folder", async () => {
    const listFilesFn = vi
      .fn()
      .mockResolvedValueOnce(
        response({
          total: 1,
          content: [
            {
              name: "Movies",
              size: 0,
              is_dir: true,
              modified: "2026-01-02T03:04:05Z",
              created: "2026-01-02T03:04:05Z",
              sign: "",
              thumb: "",
              type: 1,
              hashinfo: "",
            },
          ],
        }),
      )
      .mockResolvedValueOnce(response())

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click(await screen.findByLabelText("Open folder Movies"))

    await waitFor(() => {
      expect(window.location.search).toBe("?path=%2FMovies")
    })
    expect(listFilesFn).toHaveBeenLastCalledWith({
      path: "/Movies",
      refresh: false,
    })
  })

  it("does not open a folder when clicking a row action", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 1,
        content: [
          {
            name: "Movies",
            size: 0,
            is_dir: true,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "",
            thumb: "",
            type: 1,
            hashinfo: "",
          },
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click(await screen.findByLabelText("Rename Movies"))

    expect(window.location.search).toBe("")
    expect(screen.getByRole("heading", { name: "Rename item" })).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toHaveClass(
      "grid-rows-[auto_minmax(0,1fr)_auto]",
      "overflow-hidden",
    )
    expect(screen.getByLabelText("New name").closest('[data-slot="dialog-body"]'))
      .toHaveClass("overflow-y-auto")
  })

  it("shows media search for video files and folders only", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [
          {
            name: "Movies",
            size: 0,
            is_dir: true,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "",
            thumb: "",
            type: 1,
            hashinfo: "",
          },
          {
            name: "movie.mkv",
            size: 1024,
            is_dir: false,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "signed",
            thumb: "",
            type: 2,
            hashinfo: "",
          },
          {
            name: "notes.txt",
            size: 128,
            is_dir: false,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "signed",
            thumb: "",
            type: 2,
            hashinfo: "",
          },
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    await userEvent.click(await screen.findByLabelText("Rename movie.mkv"))
    expect(screen.getByRole("button", { name: "Media search" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }))

    await userEvent.click(screen.getByLabelText("Rename Movies"))
    expect(screen.getByRole("button", { name: "Media search" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }))

    await userEvent.click(screen.getByLabelText("Rename notes.txt"))
    expect(screen.queryByRole("button", { name: "Media search" })).not.toBeInTheDocument()
  })

  it("does not expose music metadata embedding from OpenList files", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [
          item("song.flac", { sign: "signed" }),
          item("audio.xyz"),
          item("Music", { is_dir: true }),
        ],
      }),
    )

    renderWithProviders(
      <FileBrowser
        openListBaseURL="http://openlist.local"
        listFilesFn={listFilesFn}
      />,
    )

    expect((await screen.findAllByText("song.flac")).length).toBeGreaterThan(0)
    expect(
      screen.queryByLabelText("Edit music metadata for song.flac"),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByLabelText("Edit music metadata for Music"),
    ).not.toBeInTheDocument()
    const songActions = screen.getByLabelText("Actions for song.flac")
    expect(
      Array.from(songActions.querySelectorAll<HTMLElement>("button, a")).map(
        (action) => action.getAttribute("aria-label"),
      ),
    ).toEqual([
      "Rename song.flac",
      "Copy song.flac",
      "Move song.flac",
      "Download song.flac",
      "Delete song.flac",
    ])
    const mobileSongActions = screen.getByLabelText("Actions for song.flac mobile")
    expect(
      Array.from(mobileSongActions.querySelectorAll<HTMLElement>("button, a")).map(
        (action) => action.getAttribute("aria-label"),
      ),
    ).toEqual([
      "Rename song.flac mobile",
      "Copy song.flac mobile",
      "Move song.flac mobile",
      "Download song.flac mobile",
      "Delete song.flac mobile",
    ])
  })

  it("renders the empty state", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(response())

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    expect(await screen.findByText("This folder is empty")).toBeInTheDocument()
  })

  it("renders an error state with recovery guidance", async () => {
    const listFilesFn = vi
      .fn()
      .mockRejectedValue(new ManagerApiError("session expired", 401))

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    expect(await screen.findByText("Unable to load this folder")).toBeInTheDocument()
    expect(screen.getByText("session expired")).toBeInTheDocument()
    expect(screen.getByText("Sign in to the manager again.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Retry/i })).toBeInTheDocument()
  })

  it("submits rename operations through the manager API", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 200,
          message: "success",
          data: null,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 1,
        content: [
          {
            name: "readme.txt",
            size: 2048,
            is_dir: false,
            modified: "2026-01-02T03:04:05Z",
            created: "2026-01-02T03:04:05Z",
            sign: "signed",
            thumb: "",
            type: 2,
            hashinfo: "",
          },
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click(await screen.findByLabelText("Rename readme.txt"))
    const input = screen.getByLabelText("New name")
    await userEvent.clear(input)
    await userEvent.type(input, "renamed.txt")
    await userEvent.click(screen.getByRole("button", { name: "Rename item" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/openlist/fs/rename",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            path: "/readme.txt",
            name: "renamed.txt",
            overwrite: false,
          }),
        }),
      )
    })
    fetchMock.mockRestore()
  })

  it("supports selection, indeterminate select-all, and clearing without opening folders", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 2,
        content: [
          item("Movies", { is_dir: true }),
          item("readme.txt"),
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    expect(screen.queryByRole("toolbar", { name: "Bulk file actions" })).not.toBeInTheDocument()
    await userEvent.click((await screen.findAllByLabelText("Select Movies"))[0])

    expect(window.location.search).toBe("")
    const toolbar = screen.getByRole("toolbar", { name: "Bulk file actions" })
    expect(toolbar).toBeInTheDocument()
    expect(
      within(toolbar)
        .getAllByRole("button")
        .slice(0, 4)
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["Batch rename", "Copy", "Move", "Delete"])
    expect(within(toolbar).queryByText("Batch rename")).not.toBeInTheDocument()
    const [renameButton, copyButton, moveButton] = within(toolbar)
      .getAllByRole("button")
      .slice(0, 3)
    expect(renameButton).toHaveClass("text-file-text")
    expect(copyButton).toHaveClass("text-file-document")
    expect(moveButton).toHaveClass("text-file-audio")
    const deleteButton = within(toolbar).getByRole("button", { name: "Delete" })
    expect(deleteButton).toHaveAttribute("data-variant", "ghost")
    expect(deleteButton).toHaveClass("text-destructive")
    expect(screen.getAllByLabelText("Select all")[0]).toHaveAttribute(
      "data-state",
      "indeterminate",
    )

    await userEvent.click(screen.getAllByLabelText("Select all")[0])
    for (const checkbox of screen.getAllByLabelText(/Select (Movies|readme\.txt)/)) {
      expect(checkbox).toHaveAttribute("data-state", "checked")
    }

    await userEvent.click(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByRole(
        "button",
        { name: "Clear selection" },
      ),
    )
    expect(screen.queryByRole("toolbar", { name: "Bulk file actions" })).not.toBeInTheDocument()
  })

  it("selects multiple rows by dragging from a checkbox", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [item("Alpha.mkv"), item("Beta.mkv"), item("Gamma.mkv")],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    const first = (await screen.findAllByLabelText("Select Alpha.mkv"))[0]
    const secondRow = screen.getAllByLabelText("Select Beta.mkv")[0].closest(
      "[data-selection-key]",
    )
    const thirdRow = screen.getAllByLabelText("Select Gamma.mkv")[0].closest(
      "[data-selection-key]",
    )

    fireEvent.pointerDown(first, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: 0,
      clientY: 0,
    })
    fireEvent.pointerMove(document, {
      pointerId: 1,
      pointerType: "mouse",
      clientX: 0,
      clientY: 10,
    })
    fireEvent.pointerEnter(secondRow as Element, { pointerId: 1, pointerType: "mouse" })
    fireEvent.pointerEnter(thirdRow as Element, { pointerId: 1, pointerType: "mouse" })
    fireEvent.pointerUp(document, { pointerId: 1, pointerType: "mouse" })

    for (const name of ["Alpha.mkv", "Beta.mkv", "Gamma.mkv"]) {
      expect(screen.getAllByLabelText(`Select ${name}`)[0]).toHaveAttribute(
        "data-state",
        "checked",
      )
    }
    expect(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByText("3"),
    ).toBeInTheDocument()
  })

  it("keeps the drag selection continuous when moving back toward the anchor", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [item("Alpha.mkv"), item("Beta.mkv"), item("Gamma.mkv")],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    const first = (await screen.findAllByLabelText("Select Alpha.mkv"))[0]
    const firstRow = first.closest("[data-selection-key]")
    const secondRow = screen.getAllByLabelText("Select Beta.mkv")[0].closest(
      "[data-selection-key]",
    )
    const thirdRow = screen.getAllByLabelText("Select Gamma.mkv")[0].closest(
      "[data-selection-key]",
    )

    fireEvent.pointerDown(first, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: 0,
      clientY: 0,
    })
    fireEvent.pointerMove(document, {
      pointerId: 1,
      pointerType: "mouse",
      clientX: 0,
      clientY: 10,
    })
    fireEvent.pointerEnter(thirdRow as Element, { pointerId: 1, pointerType: "mouse" })

    for (const name of ["Alpha.mkv", "Beta.mkv", "Gamma.mkv"]) {
      expect(screen.getAllByLabelText(`Select ${name}`)[0]).toHaveAttribute(
        "data-state",
        "checked",
      )
    }

    fireEvent.pointerEnter(secondRow as Element, { pointerId: 1, pointerType: "mouse" })
    expect(screen.getAllByLabelText("Select Alpha.mkv")[0]).toHaveAttribute(
      "data-state",
      "checked",
    )
    expect(screen.getAllByLabelText("Select Beta.mkv")[0]).toHaveAttribute(
      "data-state",
      "checked",
    )
    expect(screen.getAllByLabelText("Select Gamma.mkv")[0]).toHaveAttribute(
      "data-state",
      "unchecked",
    )

    fireEvent.pointerEnter(firstRow as Element, { pointerId: 1, pointerType: "mouse" })
    fireEvent.pointerUp(document, { pointerId: 1, pointerType: "mouse" })

    expect(screen.getAllByLabelText("Select Alpha.mkv")[0]).toHaveAttribute(
      "data-state",
      "checked",
    )
    for (const name of ["Beta.mkv", "Gamma.mkv"]) {
      expect(screen.getAllByLabelText(`Select ${name}`)[0]).toHaveAttribute(
        "data-state",
        "unchecked",
      )
    }
    expect(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByText("1"),
    ).toBeInTheDocument()
  })

  it("continuously clears checked rows and restores them when reversing", async () => {
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [item("Alpha.mkv"), item("Beta.mkv"), item("Gamma.mkv")],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)

    await userEvent.click((await screen.findAllByLabelText("Select all"))[0])
    const first = screen.getAllByLabelText("Select Alpha.mkv")[0]
    const secondRow = screen.getAllByLabelText("Select Beta.mkv")[0].closest(
      "[data-selection-key]",
    )
    const thirdRow = screen.getAllByLabelText("Select Gamma.mkv")[0].closest(
      "[data-selection-key]",
    )

    fireEvent.pointerDown(first, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: 0,
      clientY: 0,
    })
    fireEvent.pointerMove(document, {
      pointerId: 1,
      pointerType: "mouse",
      clientX: 0,
      clientY: 10,
    })
    fireEvent.pointerEnter(thirdRow as Element, { pointerId: 1, pointerType: "mouse" })

    for (const name of ["Alpha.mkv", "Beta.mkv", "Gamma.mkv"]) {
      expect(screen.getAllByLabelText(`Select ${name}`)[0]).toHaveAttribute(
        "data-state",
        "unchecked",
      )
    }

    fireEvent.pointerEnter(secondRow as Element, { pointerId: 1, pointerType: "mouse" })
    fireEvent.pointerUp(document, { pointerId: 1, pointerType: "mouse" })

    for (const name of ["Alpha.mkv", "Beta.mkv"]) {
      expect(screen.getAllByLabelText(`Select ${name}`)[0]).toHaveAttribute(
        "data-state",
        "unchecked",
      )
    }
    expect(screen.getAllByLabelText("Select Gamma.mkv")[0]).toHaveAttribute(
      "data-state",
      "checked",
    )
  })

  it("moves selected items in display order", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 200, message: "success", data: null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({
        total: 3,
        content: [
          item("Two.mkv"),
          item("Archive", { is_dir: true }),
          item("One.mkv"),
        ],
      }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click((await screen.findAllByLabelText("Select One.mkv"))[0])
    await userEvent.click(screen.getAllByLabelText("Select Two.mkv")[0])

    await userEvent.click(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByRole(
        "button",
        { name: "Move" },
      ),
    )
    await userEvent.click(await screen.findByLabelText("Select folder Archive"))
    await userEvent.click(screen.getByRole("button", { name: "Move items" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/openlist/fs/move",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            overwrite: false,
            skip_existing: false,
            merge: false,
            src_dir: "/",
            dst_dir: "/Archive",
            names: ["One.mkv", "Two.mkv"],
          }),
        }),
      )
    })
    expect(screen.queryByRole("toolbar", { name: "Bulk file actions" })).not.toBeInTheDocument()
    fetchMock.mockRestore()
  })

  it("confirms deletion before submitting selected names", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 200, message: "success", data: null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({ total: 1, content: [item("Movie.mkv")] }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click((await screen.findAllByLabelText("Select Movie.mkv"))[0])
    await userEvent.click(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByRole(
        "button",
        { name: "Delete" },
      ),
    )

    expect(fetchMock).not.toHaveBeenCalled()
    const dialog = screen.getByRole("dialog")
    expect(within(dialog).getByText("Permanently delete 1 selected items?")).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/openlist/fs/remove",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ dir: "/", names: ["Movie.mkv"] }),
        }),
      )
    })
    fetchMock.mockRestore()
  })

  it("confirms a row deletion and submits only that item", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 200, message: "success", data: null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({ total: 2, content: [item("Keep.mkv"), item("Remove.mkv")] }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click(await screen.findByLabelText("Delete Remove.mkv"))

    expect(fetchMock).not.toHaveBeenCalled()
    const dialog = screen.getByRole("dialog")
    expect(
      within(dialog).getByText("Permanently delete 1 selected items?"),
    ).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/openlist/fs/remove",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ dir: "/", names: ["Remove.mkv"] }),
        }),
      )
    })
    fetchMock.mockRestore()
  })

  it("previews and submits sequential batch rename plans", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 200, message: "success", data: null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({ total: 2, content: [item("Two.mkv"), item("One.mp4")] }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click((await screen.findAllByLabelText("Select One.mp4"))[0])
    await userEvent.click(screen.getAllByLabelText("Select Two.mkv")[0])
    await userEvent.click(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByRole(
        "button",
        { name: "Batch rename" },
      ),
    )
    await userEvent.click(screen.getByRole("tab", { name: "Sequential" }))
    await userEvent.type(screen.getByLabelText("Name template"), "Episode-{number}")
    await userEvent.clear(screen.getByLabelText("Start number"))
    await userEvent.type(screen.getByLabelText("Start number"), "1")
    await userEvent.clear(screen.getByLabelText("Zero padding"))
    await userEvent.type(screen.getByLabelText("Zero padding"), "2")
    await userEvent.click(screen.getByRole("button", { name: "Preview" }))

    expect(screen.getByText("Episode-01.mp4")).toBeInTheDocument()
    expect(screen.getByText("Episode-02.mkv")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Rename" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/openlist/fs/batch_rename",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            src_dir: "/",
            rename_objects: [
              { src_name: "One.mp4", new_name: "Episode-01.mp4" },
              { src_name: "Two.mkv", new_name: "Episode-02.mkv" },
            ],
          }),
        }),
      )
    })
    fetchMock.mockRestore()
  })

  it("keeps the selection when a bulk operation fails", async () => {
    const fetchMock = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 500, message: "remove failed", data: null }), {
        status: 500,
        headers: { "Content-Type": "application/json" },
      }),
    )
    const listFilesFn = vi.fn().mockResolvedValue(
      response({ total: 1, content: [item("Movie.mkv")] }),
    )

    renderWithProviders(<FileBrowser listFilesFn={listFilesFn} />)
    await userEvent.click((await screen.findAllByLabelText("Select Movie.mkv"))[0])
    await userEvent.click(
      within(screen.getByRole("toolbar", { name: "Bulk file actions" })).getByRole(
        "button",
        { name: "Delete" },
      ),
    )
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))

    expect(await screen.findByText("remove failed")).toBeInTheDocument()
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }))
    expect(screen.getByRole("toolbar", { name: "Bulk file actions" })).toBeInTheDocument()
    expect(screen.getAllByLabelText("Select Movie.mkv")[0]).toHaveAttribute(
      "data-state",
      "checked",
    )
    fetchMock.mockRestore()
  })
})
