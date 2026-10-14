import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { RotateCcw, Square, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  cancelTaskRun,
  getTaskRun,
  retryTaskRun,
  type TaskRun,
  type TaskType,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { formatDate } from "@/shared/lib/format"
import { taskQueryKeys } from "./queryKeys"
import {
  getProgressLabel,
  getTaskPollingInterval,
  getTaskSubject,
  getTaskTypeLabel,
  isTaskActive,
  isTaskRetryable,
} from "./taskModel"
import { TaskStatusBadge } from "./TaskStatusBadge"

interface TaskRunDetailDialogProps {
  runId: number | null
  types: TaskType[]
  onTaskCreated?: (run: TaskRun) => void
  onDelete: (run: TaskRun) => void
  onRunChange: (runId: number | null) => void
}

export function TaskRunDetailDialog({
  runId,
  types,
  onTaskCreated,
  onDelete,
  onRunChange,
}: TaskRunDetailDialogProps) {
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const runQuery = useQuery({
    queryKey: taskQueryKeys.run(runId ?? 0),
    queryFn: () => getTaskRun(runId ?? 0),
    enabled: runId !== null,
    refetchInterval: (query) =>
      getTaskPollingInterval(isTaskActive(query.state.data?.status)),
  })

  const cancelMutation = useMutation({
    mutationFn: cancelTaskRun,
    onSuccess: (run) => {
      queryClient.setQueryData(taskQueryKeys.run(run.id), run)
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      toast.success(t("tasks.cancelRequested"))
    },
  })
  const retryMutation = useMutation({
    mutationFn: retryTaskRun,
    onSuccess: (run) => {
      queryClient.setQueryData(taskQueryKeys.run(run.id), run)
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      toast.success(t("tasks.retryCreated"))
      onTaskCreated?.(run)
      onRunChange(run.id)
    },
  })

  const run = runQuery.data
  const descriptor = types.find((item) => item.type === run?.type)
  const canCancel =
    Boolean(run && isTaskActive(run.status)) && descriptor?.cancelable === true
  const canRetry = run !== undefined && isTaskRetryable(run, types)
  const canDelete = Boolean(run && run.status !== "running")
  const mutationError = cancelMutation.error ?? retryMutation.error

  return (
    <Dialog
      open={runId !== null}
      onOpenChange={(open) => {
        if (!open && !cancelMutation.isPending && !retryMutation.isPending) {
          onRunChange(null)
        }
      }}
    >
      <DialogContent
        className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-2xl"
      >
        <DialogHeader className="px-4 pt-4 sm:px-6 sm:pt-6">
          <DialogTitle>
            {t("tasks.detail.title", { id: runId ?? "" })}
          </DialogTitle>
        </DialogHeader>
        <DialogBody className="overscroll-contain px-4 pb-4 sm:px-6">
          {runQuery.isLoading ? <TaskRunDetailSkeleton /> : null}
          {runQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription>
                {(runQuery.error as Error).message}
              </AlertDescription>
            </Alert>
          ) : null}
          {run ? (
            <div className="space-y-5">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <TaskStatusBadge status={run.status} />
                <Badge variant="secondary">
                  {getTaskTypeLabel(run.type, t)}
                </Badge>
                {run.cancel_requested ? (
                  <Badge variant="outline">{t("tasks.cancelPending")}</Badge>
                ) : null}
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="min-w-0 truncate font-medium">
                    {run.progress.stage === "scanning" ? (
                      <Spinner className="mr-1.5 inline size-4" />
                    ) : null}
                    {getProgressLabel(run, t)}
                  </span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {run.progress.percent}%
                  </span>
                </div>
                <Progress
                  aria-label={t("tasks.progress")}
                  value={run.progress.percent}
                />
              </div>

              <dl className="grid gap-x-6 gap-y-4 border-t pt-5 sm:grid-cols-2">
                <TaskDetail
                  label={t("tasks.subject")}
                  value={getTaskSubject(run, t)}
                />
                <TaskDetail
                  label={t("tasks.trigger")}
                  value={t(`tasks.trigger.${run.trigger}`)}
                />
                <TaskDetail
                  label={t("tasks.attempt")}
                  value={t("tasks.attemptValue", {
                    current: run.attempt,
                    total: run.max_attempts,
                  })}
                />
                <TaskDetail
                  label={t("tasks.createdAt")}
                  value={formatDate(run.created_at, locale)}
                />
                <TaskDetail
                  label={t("tasks.startedAt")}
                  value={
                    run.started_at ? formatDate(run.started_at, locale) : "-"
                  }
                />
                <TaskDetail
                  label={t("tasks.finishedAt")}
                  value={
                    run.finished_at ? formatDate(run.finished_at, locale) : "-"
                  }
                />
                <TaskDetail
                  label={t("tasks.availableAt")}
                  value={formatDate(run.available_at, locale)}
                />
              </dl>

              {run.error ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    <span className="font-medium">{run.error.code}</span>
                    <span className="ml-2">{run.error.message}</span>
                  </AlertDescription>
                </Alert>
              ) : null}

              {run.result !== undefined ? <TaskResult run={run} /> : null}

              {mutationError ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    {(mutationError as Error).message}
                  </AlertDescription>
                </Alert>
              ) : null}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter className="mx-0 mb-0 rounded-none px-4 sm:px-6">
          <Button
            type="button"
            variant="outline"
            onClick={() => onRunChange(null)}
            disabled={cancelMutation.isPending || retryMutation.isPending}
          >
            {t("tasks.close")}
          </Button>
          {canCancel && run ? (
            <Button
              type="button"
              variant="outline"
              disabled={cancelMutation.isPending}
              onClick={() => cancelMutation.mutate(run.id)}
            >
              {cancelMutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Square data-icon="inline-start" />
              )}
              {t("tasks.cancel")}
            </Button>
          ) : null}
          {canRetry && run ? (
            <Button
              type="button"
              disabled={retryMutation.isPending}
              onClick={() => retryMutation.mutate(run.id)}
            >
              {retryMutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RotateCcw data-icon="inline-start" />
              )}
              {t("tasks.retry")}
            </Button>
          ) : null}
          {canDelete && run ? (
            <Button
              type="button"
              variant="destructive"
              onClick={() => onDelete(run)}
            >
              <Trash2 data-icon="inline-start" />
              {t("tasks.deleteRun")}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TaskDetail({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 break-words text-sm font-medium">{value}</dd>
    </div>
  )
}

function TaskResult({ run }: { run: TaskRun }) {
  const { t } = useI18n()
  if (
    run.type === "music.embed_metadata" &&
    typeof run.result === "object" &&
    run.result !== null
  ) {
    const embeddedCover =
      "embedded_cover" in run.result && run.result.embedded_cover === true
    const embeddedLyrics =
      "embedded_lyrics" in run.result && run.result.embedded_lyrics === true
    return (
      <div className="space-y-2 border-t pt-5">
        <div className="text-xs text-muted-foreground">{t("tasks.result")}</div>
        <div className="flex flex-wrap gap-2">
          <Badge variant={embeddedCover ? "default" : "secondary"}>
            {t("music.cover")}
          </Badge>
          <Badge variant={embeddedLyrics ? "default" : "secondary"}>
            {t("music.lyrics")}
          </Badge>
        </div>
      </div>
    )
  }

  if (
    run.type === "media.scan" &&
    typeof run.result === "object" &&
    run.result !== null
  ) {
    const value = run.result as Record<string, unknown>
    const summaries = [
      { key: "added", label: "tasks.mediaScan.added", variant: "default" },
      { key: "updated", label: "tasks.mediaScan.updated", variant: "secondary" },
      { key: "removed", label: "tasks.mediaScan.removed", variant: "outline" },
      { key: "metadata_failed", label: "tasks.mediaScan.metadataFailed", variant: "destructive" },
      { key: "screenshots_generated", label: "tasks.mediaScan.screenshotsGenerated", variant: "default" },
      { key: "screenshots_failed", label: "tasks.mediaScan.screenshotsFailed", variant: "destructive" },
    ] as const
    return (
      <div className="space-y-2 border-t pt-5">
        <div className="text-xs text-muted-foreground">{t("tasks.result")}</div>
        <div className="flex flex-wrap gap-2">
          {summaries.map(({ key, label, variant }) => (
            <Badge key={key} variant={variant}>
              {t(label, {
                count: typeof value[key] === "number" ? value[key] : 0,
              })}
            </Badge>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-2 border-t pt-5">
      <div className="text-xs text-muted-foreground">{t("tasks.result")}</div>
      <pre className="max-h-48 overflow-auto rounded-lg bg-muted p-3 text-xs">
        {JSON.stringify(run.result, null, 2)}
      </pre>
    </div>
  )
}

function TaskRunDetailSkeleton() {
  return (
    <div className="space-y-5">
      <div className="flex gap-2">
        <Skeleton className="h-5 w-20" />
        <Skeleton className="h-5 w-36" />
      </div>
      <Skeleton className="h-2 w-full" />
      <div className="grid gap-4 sm:grid-cols-2">
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton className="h-12 w-full" key={index} />
        ))}
      </div>
    </div>
  )
}
