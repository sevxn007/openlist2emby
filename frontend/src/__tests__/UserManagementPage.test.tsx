import {screen, waitFor} from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest"

import {UserManagementPage} from "@/features/users/UserManagementPage"
import {renderWithProviders} from "@/test/render"

const fetchMock = vi.fn()

vi.stubGlobal("fetch", fetchMock)

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: status, message: "success", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const viewer = {
  id: 2,
  openlist_user_id: 2, upstream_status: "active",
  username: "viewer",
  role: "media",
  enabled: true,
  library_grants: [{ library_id: 7, access: "view" }],
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

const library = {
  id: 7,
  name: "Movies",
  content_type: "movies",
  root_paths: ["/Media/Movies"],
  created_at: "2026-08-01T00:00:00Z",
  updated_at: "2026-08-01T00:00:00Z",
}

beforeEach(() => {
  window.localStorage.setItem("openlist2emby.language", "en")
})

afterEach(() => {
  fetchMock.mockReset()
  window.localStorage.clear()
})

describe("UserManagementPage", () => {
  it("grants an OpenList identity with explicit library access", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "/api/manager/users" && !init?.method) {
        return Promise.resolve(envelope({ items: [viewer], total: 1 }))
      }
      if (url.startsWith("/api/manager/openlist/users?")) { return Promise.resolve(envelope({content:[{id:12,username:"living-room",role:0,disabled:false}],total:1})) }
      if (url === "/api/manager/media-libraries?include_hidden=true") {
        return Promise.resolve(envelope({ items: [library], total: 1 }))
      }
      if (url === "/api/manager/user-authorizations" && init?.method === "POST") {
        return Promise.resolve(
          envelope(
            {
              ...viewer,
              id: 3,
              username: "living-room",
            },
            201,
          ),
        )
      }
      return Promise.resolve(envelope(null, 404))
    })
    const user = userEvent.setup()

    renderWithProviders(<UserManagementPage currentUserID={1} />)

    await screen.findByText("viewer")
    expect(fetchMock).not.toHaveBeenCalledWith(
      "/api/manager/media-libraries?include_hidden=true",
      expect.anything(),
    )

    await user.click(screen.getByRole("button", { name: "Grant access" }))
    const dialog = await screen.findByRole("dialog", { name: "Grant access" })
    expect(dialog).toBeInTheDocument()
    expect(dialog).toHaveClass(
      "grid-rows-[auto_minmax(0,1fr)_auto]",
      "overflow-hidden",
    )
    expect(dialog.querySelector('[data-slot="dialog-body"]')).toHaveClass(
      "overflow-y-auto",
    )
    await user.click(await screen.findByRole("combobox", {name:"Select OpenList user"}))
    await user.click(await screen.findByRole("option", {name:"living-room"}))
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument()
    await user.click(await screen.findByRole("checkbox", { name: "Movies" }))
      expect(screen.queryByRole("combobox", {name: "Access for Movies"})).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save user" }))

    await waitFor(() => {
      const createCall = fetchMock.mock.calls.find(
        ([url, init]) =>
          String(url) === "/api/manager/user-authorizations" &&
          (init as RequestInit | undefined)?.method === "POST",
      )
      expect(createCall).toBeDefined()
      expect(JSON.parse(String((createCall?.[1] as RequestInit).body))).toEqual({
        openlist_user_id: 12,
        role: "media",
        enabled: true,
          library_grants: [{library_id: 7, access: "view"}],
      })
    })
  })

  it("lets an administrator save an empty visible library selection", async () => {
    const administrator = {
      id: 1,
      openlist_user_id: 1, upstream_status: "active",
      username: "admin",
      role: "admin",
      enabled: true,
      library_grants: [{ library_id: 7, access: "view" }],
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z",
    }
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "/api/manager/users" && !init?.method) {
        return Promise.resolve(envelope({ items: [administrator], total: 1 }))
      }
      if (url.startsWith("/api/manager/openlist/users?")) { return Promise.resolve(envelope({content:[{id:12,username:"living-room",role:0,disabled:false}],total:1})) }
      if (url === "/api/manager/media-libraries?include_hidden=true") {
        return Promise.resolve(envelope({ items: [library], total: 1 }))
      }
      if (url === "/api/manager/users/1" && init?.method === "PUT") {
        return Promise.resolve(envelope({ ...administrator, library_grants: [] }))
      }
      return Promise.resolve(envelope(null, 404))
    })
    const user = userEvent.setup()

    renderWithProviders(<UserManagementPage currentUserID={1} />)

    await screen.findByText("admin")
    await user.click(screen.getByRole("button", { name: "Edit admin" }))
    const libraryCheckbox = await screen.findByRole("checkbox", { name: "Movies" })
    expect(libraryCheckbox).toBeChecked()
    expect(
      screen.queryByRole("combobox", { name: "Access for Movies" }),
    ).not.toBeInTheDocument()
    await user.click(libraryCheckbox)
    await user.click(screen.getByRole("button", { name: "Save user" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/users/1",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            role: "admin",
            enabled: true,
            library_grants: [],
          }),
        }),
      ),
    )
  })

  it("allows an administrator to disable another user but not themselves", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "/api/manager/users" && !init?.method) {
        return Promise.resolve(
          envelope({
            items: [
              {
                id: 1,
      openlist_user_id: 1, upstream_status: "active",
				username: "admin",
				role: "admin",
				enabled: true,
				library_grants: [],
                created_at: "2026-08-01T00:00:00Z",
                updated_at: "2026-08-01T00:00:00Z",
              },
              viewer,
            ],
            total: 2,
          }),
        )
      }
      if (url === "/api/manager/users/2" && init?.method === "PUT") {
        return Promise.resolve(envelope({ ...viewer, enabled: false }))
      }
      return Promise.resolve(envelope(null, 404))
    })
    const user = userEvent.setup()

    renderWithProviders(<UserManagementPage currentUserID={1} />)

    const ownSwitch = await screen.findByRole("switch", {
      name: "Change status for admin",
    })
    expect(ownSwitch).toBeDisabled()
    await user.click(
      screen.getByRole("switch", { name: "Change status for viewer" }),
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/manager/users/2",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ enabled: false }),
        }),
      ),
    )
  })
  it("filters guests, prevents disabled grants, and clears selection between pages", async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/manager/users") return Promise.resolve(envelope({ items: [], total: 0 }))
      if (url.startsWith("/api/manager/media-libraries")) return Promise.resolve(envelope({ items: [library], total: 1 }))
      if (url.startsWith("/api/manager/openlist/users")) {
        const page = new URL(url, "http://manager.local").searchParams.get("page")
        return Promise.resolve(envelope({
          content: page === "2" ? [{ id: 51, username: "page-two", role: 0, disabled: false }] : [
            { id: 1, username: "guest", role: 1, disabled: false },
            { id: 2, username: "disabled", role: 0, disabled: true },
            { id: 3, username: "available", role: 0, disabled: false },
          ],
          total: 51,
        }))
      }
      return Promise.resolve(envelope(null, 404))
    })
    const user = userEvent.setup()
    renderWithProviders(<UserManagementPage currentUserID={1} />)
    await user.click((await screen.findAllByRole("button", { name: "Grant access" }))[0])
    await user.click(await screen.findByRole("combobox", { name: "Select OpenList user" }))
    expect(screen.queryByRole("option", { name: "guest" })).not.toBeInTheDocument()
    expect(screen.getByRole("option", { name: "disabled (OpenList disabled)" })).toHaveAttribute("aria-disabled", "true")
    await user.click(screen.getByRole("option", { name: "available" }))
    await user.click(screen.getByRole("button", { name: "Next page" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/manager/openlist/users?page=2&per_page=50", expect.anything()))
    await user.click(screen.getByRole("button", { name: "Save user" }))
    expect(screen.getAllByText("Select OpenList user").length).toBeGreaterThan(1)
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === "POST")).toBe(false)
    await user.click(screen.getByRole("combobox", { name: "Select OpenList user" }))
    expect(await screen.findByRole("option", { name: "page-two" })).toBeInTheDocument()
  })

})
