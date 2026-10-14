import { useState } from "react"
import { act, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { MusicMetadataDialog } from "@/features/media/MusicMetadataDialog"
import { renderWithProviders } from "@/test/render"

function envelope(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({ code: status, message: "success", data }),
    { status, headers: { "Content-Type": "application/json" } },
  )
}

function deferredResponse() {
  let resolve: (response: Response) => void = () => undefined
  const promise = new Promise<Response>((nextResolve) => {
    resolve = nextResolve
  })
  return {
    promise,
    resolve,
  }
}

const target = {
	id: 17,
  name: "Ocean.Eyes.flac",
  size: 1024,
  is_dir: false,
  modified: "2026-01-02T03:04:05Z",
  created: "2026-01-02T03:04:05Z",
  sign: "signed",
  thumb: "",
  type: 2,
  hashinfo: "",
}

beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe("MusicMetadataDialog", () => {
	it("searches and embeds from Kuwo Music", async () => {
		const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input, init) => {
			const url = String(input)
			if (url === "/api/manager/music/search?query=Ocean+Eyes&source=kuwo") {
				return Promise.resolve(envelope({ results: [{
					id: "kuwo-song", name: "Ocean Eyes", artists: ["Billie Eilish"], album: "dont smile at me",
					picture_id: "kuwo-song", lyric_id: "kuwo-song", source: "kuwo",
				}] }))
			}
			if (url.startsWith("/api/manager/music/detail?source=kuwo")) {
				return Promise.resolve(envelope({ cover_url: "https://img.kuwo.cn/cover.jpg", lyrics: "Kuwo lyrics" }))
			}
			if (url === "/api/manager/tasks/runs" && init?.method === "POST") {
				return Promise.resolve(envelope({
					id: 51, type: "music.embed_metadata", status: "succeeded", progress: { stage: "completed", percent: 100 },
					result: { embedded_cover: true, embedded_lyrics: true },
				}, 202))
			}
			return Promise.resolve(envelope(null, 404))
		})

		renderWithProviders(<MusicMetadataDialog open target={target} onClose={vi.fn()} />)
		await userEvent.click(screen.getByLabelText("Music source"))
		await userEvent.click(screen.getByRole("option", { name: "Kuwo Music" }))
		await userEvent.click(screen.getByRole("button", { name: "Search" }))
		await userEvent.click(await screen.findByRole("button", { name: /Ocean Eyes Billie Eilish/ }))
		expect(await screen.findByText("Kuwo lyrics")).toBeInTheDocument()
		await userEvent.click(screen.getByRole("button", { name: "Embed metadata" }))

		await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
			"/api/manager/tasks/runs",
			expect.objectContaining({ body: expect.stringContaining('"source":"kuwo"') }),
		))
	})

	it("keeps QQ Music available", async () => {
		const fetchMock = vi.spyOn(window, "fetch").mockImplementation((input) => {
			const url = String(input)
			if (url === "/api/manager/music/search?query=Ocean+Eyes&source=qmusic") {
				return Promise.resolve(envelope({ results: [{
					id: "qq-song", name: "Ocean Eyes", artists: ["Billie Eilish"], album: "dont smile at me",
					picture_id: "qq-album", lyric_id: "qq-song", source: "qmusic",
				}] }))
			}
			if (url.startsWith("/api/manager/music/detail?source=qmusic")) {
				return Promise.resolve(envelope({ cover_url: "https://y.qq.com/cover.jpg", lyrics: "QQ lyrics" }))
			}
			return Promise.resolve(envelope(null, 404))
		})

		renderWithProviders(<MusicMetadataDialog open target={target} onClose={vi.fn()} />)
		await userEvent.click(screen.getByLabelText("Music source"))
		await userEvent.click(screen.getByRole("option", { name: "QQ Music" }))
		await userEvent.click(screen.getByRole("button", { name: "Search" }))
		await userEvent.click(await screen.findByRole("button", { name: /Ocean Eyes Billie Eilish/ }))
		expect(await screen.findByText("QQ lyrics")).toBeInTheDocument()
		await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
			"/api/manager/music/detail?source=qmusic&picture_id=qq-album&lyric_id=qq-song",
			expect.anything(),
		))
	})

  it("submits a durable task, shows progress, and opens task details", async () => {
    const runRequest = deferredResponse()
    const runningRun = {
      id: 41,
      type: "music.embed_metadata",
      payload_version: 1,
      subject: { item_id: 17 },
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
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockResolvedValueOnce(
        envelope({
          results: [
            {
              id: "1",
              name: "Ocean Eyes",
              artists: ["Billie Eilish"],
              album: "dont smile at me",
              picture_id: "picture-1",
              lyric_id: "lyric-1",
              source: "netease",
            },
          ],
        }),
      )
      .mockResolvedValueOnce(
        envelope({
          cover_url: "https://images.example/cover.jpg",
        }),
      )
      .mockResolvedValueOnce(
        envelope({
          cover_url: "https://images.example/cover.jpg",
          lyrics: "[00:01.00]I've been watching you",
        }),
      )
      .mockResolvedValueOnce(envelope(runningRun, 202))
      .mockReturnValueOnce(runRequest.promise)
    const onClose = vi.fn()
    const onTaskCreated = vi.fn()
    const onViewTask = vi.fn()

    function Harness() {
      const [activeTarget, setActiveTarget] = useState<typeof target | null>(target)
      return (
        <MusicMetadataDialog
          open={activeTarget !== null}
          target={activeTarget}
          onClose={() => {
            onClose()
            setActiveTarget(null)
          }}
          onTaskCreated={onTaskCreated}
          onViewTask={onViewTask}
        />
      )
    }

    renderWithProviders(<Harness />)

    const searchInput = screen.getByLabelText("Song search")
    await waitFor(() => expect(searchInput).toHaveValue("Ocean Eyes"))
    expect(screen.getByRole("dialog")).toHaveClass(
      "grid-rows-[auto_minmax(0,1fr)_auto]",
      "overflow-hidden",
    )
    expect(searchInput.closest('[data-slot="dialog-body"]')).toHaveClass(
      "overflow-y-auto",
    )
    await userEvent.click(screen.getByRole("button", { name: "Search" }))

    const result = await screen.findByRole("button", {
      name: /Ocean Eyes Billie Eilish dont smile at me/,
    })
    await userEvent.click(result)

    expect(
      await screen.findByText("[00:01.00]I've been watching you"),
    ).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Cover for Ocean Eyes" })).toHaveAttribute(
      "src",
      "https://images.example/cover.jpg",
    )

    await userEvent.click(screen.getByRole("button", { name: "Embed metadata" }))

    expect(await screen.findByText("Downloading original file")).toBeInTheDocument()
    expect(
      screen.getByRole("progressbar", { name: "Metadata embedding progress" }),
    ).toHaveAttribute("aria-valuenow", "46")
    expect(onTaskCreated).toHaveBeenCalledWith(runningRun)
    expect(searchInput).toBeDisabled()
    expect(result).toBeDisabled()

    await userEvent.click(screen.getByRole("button", { name: "View task" }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onViewTask).toHaveBeenCalledWith(41)
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()

    act(() => {
      runRequest.resolve(
        envelope({
          ...runningRun,
          status: "succeeded",
          progress: { stage: "completed", percent: 100 },
          finished_at: "2026-07-22T00:00:04Z",
          result: { embedded_cover: true, embedded_lyrics: true },
        }),
      )
    })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(5))
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/manager/tasks/runs",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          type: "music.embed_metadata",
          payload: {
            item_id: 17,
            source: "netease",
            picture_id: "picture-1",
            lyric_id: "lyric-1",
            embed_cover: true,
            embed_lyrics: true,
          },
        }),
      }),
    )
  })
})
