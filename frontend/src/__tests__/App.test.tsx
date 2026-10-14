import { act, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { App } from "../App"
import { MANAGER_SESSION_EXPIRED_EVENT } from "../shared/api/manager"
import { renderWithProviders } from "../test/render"

const fetchMock = vi.fn()

vi.stubGlobal("fetch", fetchMock)

function mockEnvelope(data: unknown, status = 200, message = "success") {
  return new Response(
    JSON.stringify({
      code: status,
      message,
      data,
    }),
    {
      status,
      headers: {
        "Content-Type": "application/json",
      },
    },
  )
}

function mockAdminFileRequests(mediaLibraryEnabled: boolean) {
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url === "/api/manager/status") {
      return Promise.resolve(
        mockEnvelope({
          setup_required: false,
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
          media_library_enabled: mediaLibraryEnabled,
        }),
      )
    }
    if (url === "/api/manager/auth/me") {
      return Promise.resolve(
        mockEnvelope({
          username: "manager",
          expires_at: "2026-07-27T00:00:00Z",
          is_admin: true,
        }),
      )
    }
    if (url.startsWith("/api/manager/tasks/runs?")) {
      return Promise.resolve(
        mockEnvelope({ items: [], total: 0, page: 1, per_page: 1 }),
      )
    }
    if (url === "/api/manager/openlist/fs/list") {
      return Promise.resolve(
        mockEnvelope({
          content: [],
          total: 0,
          readme: "",
          header: "",
          write: false,
          write_content_bypass: false,
          provider: "unknown",
        }),
      )
    }
    return Promise.reject(new Error(`unexpected request: ${url}`))
  })
}

const originalWidth = window.innerWidth

afterEach(() => {
  fetchMock.mockReset()
  window.localStorage.clear()
  window.history.replaceState(null, "", "/")
  Object.defineProperty(window, "innerWidth", { configurable: true, value: originalWidth })
})

describe("App", () => {
  it.each([
    { enabled: false, verb: "hides" },
    { enabled: true, verb: "shows" },
  ])("$verb the media-library navigation", async ({ enabled }) => {
    window.localStorage.setItem("openlist2emby.language", "en")
    mockAdminFileRequests(enabled)

    renderWithProviders(<App />)
    await screen.findByRole("button", { name: "Files" })

    if (enabled) {
      expect(
        screen.getByRole("button", { name: "Media Library" }),
      ).toBeInTheDocument()
    } else {
      expect(
        screen.queryByRole("button", { name: "Media Library" }),
      ).not.toBeInTheDocument()
    }
  })

  describe.each([
    {
      language: "en",
      labels: ["Files", "Media Library", "Tasks", "Users", "Settings"],
      empty: "This folder is empty",
      toggle: "Collapse sidebar",
    },
    {
      language: "zh",
      labels: ["文件", "媒体库", "任务", "用户", "设置"],
      empty: "此文件夹为空",
      toggle: "收起侧边栏",
    },
  ])("retained $language navigation", ({ language, labels, empty, toggle }) => {
    describe.each([375, 1280])("at %ipx", (width) => {
      it.each(["sync", "music-search"])("falls back from %s to files", async (section) => {
        window.localStorage.setItem("openlist2emby.language", language)
        window.history.replaceState(null, "", `/?section=${section}`)
        Object.defineProperty(window, "innerWidth", { configurable: true, value: width })
        mockAdminFileRequests(true)
        const user = userEvent.setup()

        renderWithProviders(<App />)
        expect(await screen.findByText(empty)).toBeInTheDocument()
        if (width < 768) {
          await user.click(screen.getByRole("button", { name: toggle }))
        }

        const filesButton = await screen.findByRole("button", { name: labels[0] })
        expect(screen.getAllByText("openlist2emby").length).toBeGreaterThan(0)
        expect(window.localStorage.getItem("openlist2emby.language")).toBe(language)
        const menu = filesButton.closest("ul")
        expect(menu).not.toBeNull()
        expect(within(menu!).getAllByRole("button").map((button) => button.textContent)).toEqual(labels)
        expect(filesButton).toHaveAttribute("data-active", "true")
        expect(filesButton).toHaveClass("h-11")
        expect(
          fetchMock.mock.calls.some(([input]) => /\/sync\/|\/music\/catalog\//.test(String(input))),
        ).toBe(false)
      })
    })
  })

  it("renders the initialization page when setup is required", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock.mockResolvedValueOnce(
      mockEnvelope({
        setup_required: true,
      }),
    )

    renderWithProviders(<App />)

    expect(await screen.findByText("Initial configuration")).toBeInTheDocument()
    expect(screen.getByLabelText("OpenList address")).toBeInTheDocument()
    expect(screen.getByLabelText("Administrator key")).toHaveAttribute(
      "type",
      "password",
    )
    expect(screen.getByLabelText("Username")).toBeDisabled()
    expect(screen.getByRole("progressbar", { name: "Setup progress" })).toHaveAttribute(
      "aria-valuenow",
      "1",
    )
  })

  it("validates and navigates between initialization steps", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock.mockResolvedValueOnce(mockEnvelope({ setup_required: true }))
    const user = userEvent.setup()

    renderWithProviders(<App />)

    await screen.findByText("Initial configuration")
    await user.click(screen.getByRole("button", { name: "Next" }))
    expect(screen.getByLabelText("Username")).toBeDisabled()

    await user.type(screen.getByLabelText("Administrator key"), "admin-key")
    await user.click(screen.getByRole("button", { name: "Next" }))
    expect(screen.getByLabelText("Username")).toBeEnabled()
    expect(screen.getByLabelText("Username")).toHaveFocus()
    expect(screen.getByRole("progressbar", { name: "Setup progress" })).toHaveAttribute(
      "aria-valuenow",
      "2",
    )

    await user.click(screen.getByRole("button", { name: "Back" }))
    expect(screen.getByLabelText("OpenList address")).toBeEnabled()
    expect(screen.getByLabelText("OpenList address")).toHaveFocus()
    expect(screen.getByLabelText("Username")).toBeDisabled()
  })

  it("submits the combined initialization payload from the final step", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    let setupComplete = false
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "/api/manager/status") {
        return Promise.resolve(mockEnvelope({ setup_required: !setupComplete }))
      }
      if (url === "/api/manager/setup" && init?.method === "POST") {
        setupComplete = true
        return Promise.resolve(mockEnvelope({ setup_required: false }))
      }
      if (url === "/api/manager/auth/me") {
        return Promise.resolve(mockEnvelope(null, 401, "login required"))
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })
    const user = userEvent.setup()

    renderWithProviders(<App />)

    await screen.findByText("Initial configuration")
    await user.clear(screen.getByLabelText("OpenList address"))
    await user.type(screen.getByLabelText("OpenList address"), "http://openlist.local")
    await user.type(screen.getByLabelText("Administrator key"), "admin-key")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.type(screen.getByLabelText("Username"), "manager")
    await user.type(screen.getByLabelText("Password"), "password123")
    await user.type(screen.getByLabelText("Verification code (optional)"), "123456")
    await user.click(screen.getByRole("button", { name: "Complete setup" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/setup",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            openlist_base_url: "http://openlist.local",
            openlist_admin_key: "admin-key",
            username: "manager",
            password: "password123",
            otp_code: "123456",
          }),
        }),
      )
    })
  })

  it("renders the login page after setup", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock
      .mockResolvedValueOnce(
        mockEnvelope({
          setup_required: false,
          openlist_base_url: "http://openlist.local",
          openlist_admin_key_set: true,
        }),
      )
      .mockResolvedValueOnce(mockEnvelope(null, 401, "login required"))

    renderWithProviders(<App />)

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument()
    expect(screen.getByLabelText("Username")).toBeInTheDocument()
  })

  it("returns to login when an active session expires", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/status") {
        return Promise.resolve(
          mockEnvelope({
            setup_required: false,
            openlist_base_url: "http://openlist.local",
            media_library_enabled: false,
          }),
        )
      }
      if (url === "/api/manager/auth/me") {
        return Promise.resolve(
          mockEnvelope({ username: "manager", is_admin: true }),
        )
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(
          mockEnvelope({ items: [], total: 0, page: 1, per_page: 1 }),
        )
      }
      if (url === "/api/manager/openlist/fs/list") {
        return Promise.resolve(
          mockEnvelope({ content: [], total: 0, write: false }),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<App />)
    await screen.findByRole("button", { name: "Files" })
    act(() => {
      window.dispatchEvent(new Event(MANAGER_SESSION_EXPIRED_EVENT))
    })

    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeInTheDocument()
  })

  it("saves settings through the manager API", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "/api/manager/status") {
        return Promise.resolve(
          mockEnvelope({
            setup_required: false,
            openlist_base_url: "http://openlist.local",
            openlist_admin_key_set: true,
          }),
        )
      }
      if (url === "/api/manager/auth/me") {
        return Promise.resolve(
          mockEnvelope({
            username: "alice",
            expires_at: "2026-07-07T00:00:00Z",
            is_admin: true,
          }),
        )
      }
      if (url === "/api/manager/settings" && init?.method !== "PUT") {
        return Promise.resolve(
          mockEnvelope({
            openlist_base_url: "http://openlist.local",
            openlist_admin_key_set: true,
            openlist_admin_key: "********",
          }),
        )
      }
      if (url === "/api/manager/settings" && init?.method === "PUT") {
        return Promise.resolve(
          mockEnvelope({
            openlist_base_url: "http://openlist.example",
            openlist_admin_key_set: true,
            openlist_admin_key: "********",
          }),
        )
      }
      if (url === "/api/manager/openlist/fs/list") {
        return Promise.resolve(
          mockEnvelope({
            content: [],
            total: 0,
            readme: "",
            header: "",
            write: false,
            write_content_bypass: false,
            provider: "unknown",
          }),
        )
      }
      if (url.startsWith("/api/manager/tasks/runs?")) {
        return Promise.resolve(
          mockEnvelope({ items: [], total: 0, page: 1, per_page: 1 }),
        )
      }
      return Promise.reject(new Error(`unexpected request: ${url}`))
    })

    renderWithProviders(<App />)
    await userEvent.click(await screen.findByRole("button", { name: /Settings/i }))
    expect(screen.queryByText("alice")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Logout" })).toBeInTheDocument()
    const urlInput = await screen.findByLabelText("OpenList address")
    await userEvent.clear(urlInput)
    await userEvent.type(urlInput, "http://openlist.example")
    await userEvent.type(screen.getByLabelText("Administrator key"), "new-key")
    await userEvent.click(screen.getByRole("button", { name: /Save OpenList/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/settings",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            openlist_base_url: "http://openlist.example",
            openlist_admin_key: "new-key",
          }),
        }),
      )
    })
    const storedValues = Array.from(
      { length: window.localStorage.length },
      (_, index) => window.localStorage.getItem(window.localStorage.key(index) ?? ""),
    )
    expect(storedValues).not.toContain("new-key")
  })
})
