import { screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { LoginPage } from "@/features/auth/LoginPage"
import { renderWithProviders } from "@/test/render"

const fetchMock = vi.fn()
vi.stubGlobal("fetch", fetchMock)

afterEach(() => {
  fetchMock.mockReset()
  window.localStorage.clear()
})

describe("OpenList login", () => {
  it.each([
    { language: "en", username: "Username", password: "Password", otp: "Verification code (optional)", submit: "Login" },
    { language: "zh", username: "用户名", password: "密码", otp: "二次验证码（可选）", submit: "登录" },
  ])("sends a separate verification code in $language", async (labels) => {
    window.localStorage.setItem("openlist2emby.language", labels.language)
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ code: 200, data: { username: "alice" } })))
    const complete = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(<LoginPage onLoginComplete={complete} />)
    await user.type(screen.getByLabelText(labels.username), "alice")
    await user.type(screen.getByLabelText(labels.password), "synthetic-password")
    const otp = screen.getByLabelText(labels.otp)
    expect(otp).toHaveAttribute("autocomplete", "one-time-code")
    expect(otp).not.toBeRequired()
    await user.type(otp, "123456")
    await user.click(screen.getByRole("button", { name: labels.submit }))
    await waitFor(() => expect(complete).toHaveBeenCalledOnce())
    expect(fetchMock).toHaveBeenCalledWith("/api/manager/auth/login", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ username: "alice", password: "synthetic-password", otp_code: "123456" }),
    }))
    expect(JSON.stringify(window.localStorage)).not.toContain("synthetic-password")
  })

  it("keeps login available after a temporary upstream failure", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ code: 503, message: "OpenList identity service unavailable" }), { status: 503 }))
    const complete = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(<LoginPage onLoginComplete={complete} />)
    await user.type(screen.getByLabelText("Username"), "alice")
    await user.type(screen.getByLabelText("Password"), "synthetic-password")
    await user.click(screen.getByRole("button", { name: "Login" }))
    expect(await screen.findByText("OpenList identity service unavailable")).toBeVisible()
    expect(screen.getByRole("button", { name: "Login" })).toBeEnabled()
    expect(complete).not.toHaveBeenCalled()
  })
})
