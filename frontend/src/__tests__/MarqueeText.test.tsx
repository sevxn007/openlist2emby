import { screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { MarqueeText } from "@/features/tasks/MarqueeText"
import { renderWithProviders } from "@/test/render"

const originalClientWidth = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  "clientWidth",
)
const originalScrollWidth = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  "scrollWidth",
)

function mockMarqueeWidths({
  container,
  content,
}: {
  container: number
  content: number
}) {
  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get() {
      return this.getAttribute("data-slot") === "marquee-text" ? container : 0
    },
  })
  Object.defineProperty(HTMLElement.prototype, "scrollWidth", {
    configurable: true,
    get() {
      return this.getAttribute("data-slot") === "marquee-measure" ? content : 0
    },
  })
}

afterEach(() => {
  if (originalClientWidth) {
    Object.defineProperty(HTMLElement.prototype, "clientWidth", originalClientWidth)
  }
  if (originalScrollWidth) {
    Object.defineProperty(HTMLElement.prototype, "scrollWidth", originalScrollWidth)
  }
})

describe("MarqueeText", () => {
  it("truncates text that fits the width limit", () => {
    mockMarqueeWidths({ container: 288, content: 120 })
    renderWithProviders(<MarqueeText className="w-72" text="Short name" />)

    const container = screen.getByTitle("Short name")
    expect(container).toHaveAttribute("data-overflowing", "false")
    expect(container).toHaveClass("overflow-hidden")
    expect(container.querySelector(".animate-marquee")).not.toBeInTheDocument()
    expect(container.querySelector(".truncate")).toHaveTextContent("Short name")
  })

  it("scrolls overflowing text in a marquee", () => {
    mockMarqueeWidths({ container: 288, content: 720 })
    const name =
      "/Media/Shows/A Very Long Series Title/Season 01/Episode 01.mkv"
    renderWithProviders(<MarqueeText className="w-72" text={name} />)

    const container = screen.getByTitle(name)
    expect(container).toHaveAttribute("data-overflowing", "true")
    expect(container).toHaveClass("overflow-hidden", "w-72")
    expect(container.querySelector(".animate-marquee")).toBeInTheDocument()
    const track = container.querySelector(".animate-marquee")
    expect(track).toHaveClass(
      "group-hover/marquee:[animation-play-state:paused]",
      "motion-reduce:hidden",
    )
    expect(track?.querySelectorAll("span")).toHaveLength(2)
  })
})
