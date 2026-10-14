import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { TaskRunStatus } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

const statusClassNames: Record<TaskRunStatus, string> = {
  queued: "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  running: "border-blue-500/20 bg-blue-500/10 text-blue-700 dark:text-blue-300",
  succeeded:
    "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  failed: "border-destructive/20 bg-destructive/10 text-destructive",
  canceled: "border-border bg-muted text-muted-foreground",
}

const dotClassNames: Record<TaskRunStatus, string> = {
  queued: "bg-amber-500",
  running: "animate-pulse bg-blue-500",
  succeeded: "bg-emerald-500",
  failed: "bg-destructive",
  canceled: "bg-muted-foreground",
}

export function TaskStatusBadge({ status }: { status: TaskRunStatus }) {
  const { t } = useI18n()

  return (
    <Badge
      variant="outline"
      className={cn("gap-1.5", statusClassNames[status])}
    >
      <span
        aria-hidden="true"
        className={cn("size-1.5 rounded-full", dotClassNames[status])}
      />
      {t(`tasks.status.${status}`)}
    </Badge>
  )
}
