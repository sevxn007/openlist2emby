import * as React from "react"
import { Progress as ProgressPrimitive } from "radix-ui"

import { cn } from "@/lib/utils"

const Progress = React.forwardRef<
  React.ElementRef<typeof ProgressPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof ProgressPrimitive.Root>
>(({ className, value = 0, ...props }, ref) => (
  <ProgressPrimitive.Root
    ref={ref}
    value={value}
    data-slot="progress"
    className={cn(
      "relative h-2 w-full overflow-hidden rounded-full bg-primary/15",
      className,
    )}
    {...props}
  >
    <ProgressPrimitive.Indicator
      data-slot="progress-indicator"
      className="h-full w-full bg-primary transition-transform duration-300 ease-out"
      style={{ transform: `translateX(-${100 - Math.min(Math.max(value ?? 0, 0), 100)}%)` }}
    />
  </ProgressPrimitive.Root>
))

Progress.displayName = ProgressPrimitive.Root.displayName

export { Progress }
