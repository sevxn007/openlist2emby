import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarClock, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Spinner } from "@/components/ui/spinner"
import { listTaskTypes, type TaskRun } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { taskQueryKeys } from "./queryKeys"
import { TaskRunsPanel } from "./TaskRunsPanel"
import { OpenListTasksPanel } from "./OpenListTasksPanel"

const emptyTaskTypes: Awaited<ReturnType<typeof listTaskTypes>>["items"] = []

interface TaskCenterPageProps {
  selectedRunId?: number | null
  onSelectedRunChange?: (runId: number | null) => void
  onTaskCreated?: (run: TaskRun) => void
}

export function TaskCenterPage({
  selectedRunId,
  onSelectedRunChange,
  onTaskCreated,
}: TaskCenterPageProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [internalRunId, setInternalRunId] = useState<number | null>(null)
  const resolvedRunId =
    selectedRunId === undefined ? internalRunId : selectedRunId
  const handleRunChange = onSelectedRunChange ?? setInternalRunId
  const typesQuery = useQuery({
    queryKey: taskQueryKeys.types(),
    queryFn: listTaskTypes,
  })
  const types = typesQuery.data?.items ?? emptyTaskTypes

  return (
    <Card className="w-full gap-0 overflow-hidden py-0 shadow-sm">
      <CardHeader className="flex min-h-20 flex-row items-center gap-4 border-b px-4 py-4 sm:px-5 md:px-6">
        <CardTitle className="flex min-w-0 items-center gap-2">
          <CalendarClock className="size-5 shrink-0 text-primary" />
          <span className="truncate">{t("tasks.title")}</span>
        </CardTitle>
        <CardAction>
          <Button
            type="button"
            aria-label={t("tasks.refresh")}
            size="icon-lg"
            variant="ghost"
            disabled={typesQuery.isFetching}
            onClick={() =>
              void queryClient.invalidateQueries({
                queryKey: taskQueryKeys.all,
              })
            }
          >
            {typesQuery.isFetching ? <Spinner /> : <RefreshCw />}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="p-0">
        <OpenListTasksPanel />
        <TaskRunsPanel
          selectedRunId={resolvedRunId}
          types={types}
          onSelectedRunChange={handleRunChange}
          onTaskCreated={onTaskCreated}
        />
      </CardContent>
    </Card>
  )
}
