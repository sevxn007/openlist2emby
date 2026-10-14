import { describe, expect, it } from "vitest"

import {
  type MediaItemLayout,
  mediaRailClassName,
} from "@/features/media/mediaItemPresentation"

describe("mediaRailClassName", () => {
  it.each<[MediaItemLayout, string]>([
    ["poster", "xl:auto-cols-[calc((100%-7rem)/8)]"],
    ["landscape", "xl:auto-cols-[calc((100%-3rem)/4)]"],
    ["square", "xl:auto-cols-[calc((100%-7rem)/8)]"],
    ["list", "xl:auto-cols-[calc((100%-1.5rem)/3)]"],
  ])(
    "keeps %s cards sized within a horizontal rail",
    (layout, columnClass) => {
      const className = mediaRailClassName(layout)

      expect(className).toContain(columnClass)
      expect(className).toContain("overflow-x-auto")
    },
  )
})
