import { useCallback, useEffect, useRef, useState } from "react"
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import {
  getTaskRun,
  listTaskRuns,
  MUSIC_EMBED_TASK_TYPE,
  type TaskRun,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { activeTaskFilter, taskQueryKeys } from "./queryKeys"
import {
  getTaskPollingInterval,
  isTaskActive,
  isTaskTerminal,
} from "./taskModel"

export function useTaskActivity(enabled: boolean) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [trackedRunIds, setTrackedRunIds] = useState<number[]>([])
  const notifiedRunIdsRef = useRef(new Set<number>())
  const previousActiveCountRef = useRef(0)

  const activeRunsQuery = useQuery({
    queryKey: taskQueryKeys.runs(activeTaskFilter),
    queryFn: () => listTaskRuns(activeTaskFilter),
    enabled,
    refetchInterval: (query) =>
      getTaskPollingInterval(Boolean(query.state.data?.total)),
  })

  const trackedRunQueries = useQueries({
    queries: enabled
      ? trackedRunIds.map((runId) => ({
          queryKey: taskQueryKeys.run(runId),
          queryFn: () => getTaskRun(runId),
          refetchInterval: (query: { state: { data?: TaskRun } }) =>
            getTaskPollingInterval(isTaskActive(query.state.data?.status)),
        }))
      : [],
  })

  const trackTaskRun = useCallback((run: TaskRun) => {
    setTrackedRunIds((current) =>
      current.includes(run.id) ? current : [...current, run.id],
    )
  }, [])

  useEffect(() => {
    if (enabled) {
      return
    }
    setTrackedRunIds([])
    notifiedRunIdsRef.current.clear()
    previousActiveCountRef.current = 0
  }, [enabled])

  useEffect(() => {
    const activeCount = activeRunsQuery.data?.total ?? 0
    if (previousActiveCountRef.current > activeCount) {
      void queryClient.invalidateQueries({ queryKey: ["files"] })
    }
    previousActiveCountRef.current = activeCount
  }, [activeRunsQuery.data?.total, queryClient])

  useEffect(() => {
    const finishedRunIds: number[] = []

    for (const query of trackedRunQueries) {
      const run = query.data
      if (
        !run ||
        !isTaskTerminal(run.status) ||
        notifiedRunIdsRef.current.has(run.id)
      ) {
        continue
      }

      notifiedRunIdsRef.current.add(run.id)
      finishedRunIds.push(run.id)

      if (run.type === MUSIC_EMBED_TASK_TYPE) {
        if (run.status === "succeeded") {
          toast.success(t("music.embedSuccess"))
          void queryClient.invalidateQueries({ queryKey: ["files"] })
        } else if (run.status === "failed") {
          toast.error(run.error?.message || t("music.embedFailed"))
        }
      }
    }

    if (finishedRunIds.length === 0) {
      return
    }

    const finishedRunIdSet = new Set(finishedRunIds)
    setTrackedRunIds((current) =>
      current.filter((runId) => !finishedRunIdSet.has(runId)),
    )
    void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
  }, [queryClient, t, trackedRunQueries])

  return {
    activeTaskCount: enabled ? (activeRunsQuery.data?.total ?? 0) : 0,
    trackTaskRun,
  }
}
