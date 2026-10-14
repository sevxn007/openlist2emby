import { screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import styles from "@/styles.css?raw"

import { AppShell } from "@/app/AppShell"
import { renderWithProviders } from "@/test/render"

const originalWidth = window.innerWidth

afterEach(() => {
  window.localStorage.clear()
  document.cookie = "openlist2emby_sidebar_state=; path=/; max-age=0"
  document.documentElement.classList.remove("dark")
  document.documentElement.style.colorScheme = ""
  Object.defineProperty(window, "innerWidth", { configurable: true, value: originalWidth })
})

describe.each([375, 1280])("media shell at %ipx", (width) => {
  it.each(["light", "dark"])("uses shared colors in %s mode", async (theme) => {
    window.localStorage.setItem("openlist2emby.language", "en")
    window.localStorage.setItem("openlist2emby.theme", theme)
    Object.defineProperty(window, "innerWidth", { configurable: true, value: width })
    const { container } = renderWithProviders(
      <AppShell
        activeSection="media"
        activeTaskCount={0}
        isAdmin
        mediaLibraryEnabled
        onNavigate={vi.fn()}
        onLogout={vi.fn()}
      >
        <div data-testid="media-content" />
      </AppShell>,
    )

    expect(document.documentElement.classList.contains("dark")).toBe(theme === "dark")
    expect(container.querySelector('[data-slot="sidebar-inset"]')).toHaveClass("bg-background")
    expect(container.querySelector("header")).toHaveClass("bg-card/95", "md:hidden")
    expect(screen.getByTestId("media-content").parentElement).toHaveClass("p-0")
    if (width < 768) {
      await userEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }))
    }
    const sidebar = document.querySelector(
      width < 768 ? '[data-slot="sidebar"][data-mobile="true"]' : '[data-slot="sidebar-inner"]',
    )
    expect(sidebar).toHaveClass("bg-sidebar")
    expect(await screen.findByRole("button", { name: "Media Library" })).toHaveClass(
      "data-[active=true]:bg-sidebar-primary",
      "data-[active=true]:text-sidebar-primary-foreground",
    )
    if (width >= 768) {
      await userEvent.click(screen.getAllByRole("button", { name: "Collapse sidebar" })[0])
      expect(document.cookie).toContain("openlist2emby_sidebar_state=false")
      expect(window.localStorage.getItem("openlist2emby.sidebarCollapsed")).toBe("true")
    }
  })
})

// JSDOM does not process Tailwind or resolve CSS variable references. Check the
// source token contract as well as the rendered desktop/mobile classes above.
describe("sidebar color tokens", () => {
  const lightTokens = styles.match(/:root\s*\{([^}]+)\}/)?.[1] ?? ""
  const darkTokens = styles.match(/\.dark\s*\{([^}]+)\}/)?.[1] ?? ""

  it.each([
    ["sidebar", "card"],
    ["sidebar-foreground", "foreground"],
    ["sidebar-primary", "primary"],
    ["sidebar-primary-foreground", "primary-foreground"],
    ["sidebar-accent", "accent"],
    ["sidebar-accent-foreground", "accent-foreground"],
    ["sidebar-border", "border"],
    ["sidebar-ring", "ring"],
  ])("%s follows the global %s color in both themes", (sidebarToken, globalToken) => {
    expect(lightTokens).toContain(`--${sidebarToken}: var(--${globalToken});`)
    expect(darkTokens).not.toContain(`--${sidebarToken}:`)
    const declaration = new RegExp(`--${globalToken}:\\s*([^;]+);`)
    const lightValue = lightTokens.match(declaration)?.[1]
    const darkValue = darkTokens.match(declaration)?.[1]
    expect(lightValue).toBeTruthy()
    expect(darkValue).toBeTruthy()
    expect(lightValue).not.toBe(darkValue)
  })
})
