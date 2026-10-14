import { useLayoutEffect, useRef, useState } from "react"

import { cn } from "@/lib/utils"

const MARQUEE_GAP_PX = 32
const MARQUEE_PX_PER_SECOND = 40
const MARQUEE_MIN_DURATION_SECONDS = 6

export function MarqueeText({
  text,
  className,
}: {
  text: string
  className?: string
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const measureRef = useRef<HTMLSpanElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  const [duration, setDuration] = useState(MARQUEE_MIN_DURATION_SECONDS)

  useLayoutEffect(() => {
    const container = containerRef.current
    const measure = measureRef.current
    if (!container || !measure) {
      return
    }

    const update = () => {
      const width = measure.scrollWidth
      const overflow = width - container.clientWidth
      setOverflowing(overflow > 1)
      setDuration(
        Math.max(
          MARQUEE_MIN_DURATION_SECONDS,
          (width + MARQUEE_GAP_PX) / MARQUEE_PX_PER_SECOND,
        ),
      )
    }

    update()
    const observer = new ResizeObserver(update)
    observer.observe(container)
    return () => observer.disconnect()
  }, [text])

  return (
    <div
      ref={containerRef}
      title={text}
      data-slot="marquee-text"
      data-overflowing={overflowing ? "true" : "false"}
      className={cn(
        "group/marquee relative min-w-0 overflow-hidden",
        className,
      )}
    >
      <span
        ref={measureRef}
        aria-hidden="true"
        data-slot="marquee-measure"
        className="invisible pointer-events-none absolute top-0 left-0 whitespace-nowrap"
      >
        {text}
      </span>
      {overflowing ? (
        <>
          <div
            className="flex w-max animate-marquee group-hover/marquee:[animation-play-state:paused] motion-reduce:hidden"
            style={{ animationDuration: `${duration}s` }}
          >
            <span className="pr-8 whitespace-nowrap">{text}</span>
            <span aria-hidden="true" className="pr-8 whitespace-nowrap">
              {text}
            </span>
          </div>
          <span className="hidden truncate motion-reduce:block">{text}</span>
        </>
      ) : (
        <span className="block truncate">{text}</span>
      )}
    </div>
  )
}
