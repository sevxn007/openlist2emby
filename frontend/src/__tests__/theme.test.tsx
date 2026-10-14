import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ThemeProvider, THEME_STORAGE_KEY } from "@/shared/theme/ThemeProvider"
import { useTheme } from "@/shared/theme/useTheme"
import { ThemeMenu } from "@/shared/ui/ThemeMenu"
import { renderWithProviders } from "@/test/render"

const defaultMatchMedia = window.matchMedia

afterEach(() => {
  window.localStorage.clear()
  window.matchMedia = defaultMatchMedia
  document.documentElement.classList.remove("dark")
  document.documentElement.style.colorScheme = ""
  vi.restoreAllMocks()
})

describe("theme", () => {
  it("follows the device color scheme by default", async () => {
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
      media: "(prefers-color-scheme: dark)",
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })

    render(
      <ThemeProvider>
        <ThemeState />
      </ThemeProvider>,
    )

    expect(screen.getByText("system:dark")).toBeInTheDocument()
    await waitFor(() => expect(document.documentElement).toHaveClass("dark"))
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull()
  })

  it("persists a manual dark theme selection", async () => {
    window.localStorage.setItem("openlist2emby.language", "en")
    renderWithProviders(<ThemeMenu />)

    await userEvent.click(screen.getByRole("button", { name: "Appearance" }))
    await userEvent.click(screen.getByRole("menuitemradio", { name: "Dark" }))

    await waitFor(() => expect(document.documentElement).toHaveClass("dark"))
    expect(window.localStorage.getItem("openlist2emby.theme")).toBe("dark")
  })
})

function ThemeState() {
  const { resolvedTheme, theme } = useTheme()
  return <span>{`${theme}:${resolvedTheme}`}</span>
}
