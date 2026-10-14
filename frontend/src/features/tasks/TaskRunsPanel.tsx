import { type ReactNode, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ChevronLeft,
  ChevronRight,
  CheckCheck,
  Eye,
  RotateCcw,
  Square,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  cancelTaskRun,
  deleteCompletedTaskRuns,
  deleteTaskRun,
  listTaskRuns,
  retryTaskRun,
  type TaskRun,
  type TaskRunFilter,
  type TaskRunStatus,
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
  taskRunStatuses,
} from "./taskModel"
import { TaskRunDetailDialog } from "./TaskRunDetailDialog"
import { TaskStatusBadge } from "./TaskStatusBadge"

const pageSize = 20
type StatusFilter = "all" | "active" | TaskRunStatus

interface TaskRunsPanelProps {
  selectedRunId: number | null
  types: TaskType[]
  onSelectedRunChange: (runId: number | null) => void
  onTaskCreated?: (run: TaskRun) => void
}

export function TaskRunsPanel({
  selectedRunId,
  types,
  onSelectedRunChange,
  onTaskCreated,
}: TaskRunsPanelProps) {
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all")
  const [typeFilter, setTypeFilter] = useState("all")
  const [page, setPage] = useState(1)
  const [deletingRun, setDeletingRun] = useState<TaskRun | null>(null)
  const [selectedRunIds, setSelectedRunIds] = useState<Set<number>>(() => new Set())

  const filter = useMemo<TaskRunFilter>(() => {
    const statuses =
      statusFilter === "active"
        ? (["queued", "running"] as const)
        : statusFilter === "all"
          ? undefined
          : [statusFilter]
    return {
      statuses,
      type: typeFilter === "all" ? undefined : typeFilter,
      page,
      perPage: pageSize,
    }
  }, [page, statusFilter, typeFilter])

  const runsQuery = useQuery({
    queryKey: taskQueryKeys.runs(filter),
    queryFn: () => listTaskRuns(filter),
    refetchInterval: (query) =>
      getTaskPollingInterval(
        query.state.data?.items.some((run) => isTaskActive(run.status)) ?? false,
      ),
  })

  useEffect(() => {
    setSelectedRunIds(new Set())
  }, [page, statusFilter, typeFilter])

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
      onSelectedRunChange(run.id)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: deleteTaskRun,
    onSuccess: (_, deletedRunId) => {
      queryClient.removeQueries({ queryKey: taskQueryKeys.run(deletedRunId) })
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      if (selectedRunId === deletedRunId) {
        onSelectedRunChange(null)
      }
      setSelectedRunIds((current) => {
        const next = new Set(current)
        next.delete(deletedRunId)
        return next
      })
      setDeletingRun(null)
      toast.success(t("tasks.deleteRunSuccess"))
    },
  })
  const batchMutation = useMutation({
    mutationFn: async ({ action, ids }: { action: "retry" | "delete"; ids: number[] }) => {
      const operation = action === "retry" ? retryTaskRun : deleteTaskRun
      return Promise.all(ids.map((id) => operation(id)))
    },
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      setSelectedRunIds(new Set())
      toast.success(
        variables.action === "retry"
          ? t("tasks.batchRetrySuccess")
          : t("tasks.batchDeleteSuccess"),
      )
    },
  })

  const clearCompletedMutation = useMutation({
    mutationFn: deleteCompletedTaskRuns,
    onSuccess: ({ deleted }) => {
      const selectedRun = selectedRunId === null
        ? undefined
        : queryClient.getQueryData<TaskRun>(taskQueryKeys.run(selectedRunId))
      if (selectedRun?.status === "succeeded" || selectedRun?.status === "canceled") {
        onSelectedRunChange(null)
        queryClient.removeQueries({ queryKey: taskQueryKeys.run(selectedRun.id) })
      }
      queryClient.removeQueries({
        queryKey: ["tasks", "runs"],
        type: "inactive",
        predicate: (query) => typeof query.queryKey[2] === "number",
      })
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      setSelectedRunIds(new Set())
      setPage(1)
      toast.success(t("tasks.clearCompletedSuccess", { count: deleted }))
    },
  })

  function openDelete(run: TaskRun) {
    deleteMutation.reset()
    if (selectedRunId === run.id) {
      onSelectedRunChange(null)
    }
    setDeletingRun(run)
  }

  const totalPages = Math.max(
    1,
    Math.ceil((runsQuery.data?.total ?? 0) / pageSize),
  )
  const actionError = cancelMutation.error ?? retryMutation.error ?? clearCompletedMutation.error
  const isBatchPending = batchMutation.isPending || clearCompletedMutation.isPending
  const visibleRuns = runsQuery.data?.items ?? []
  const failedRunIds = visibleRuns
    .filter((run) => run.status === "failed" && isTaskRetryable(run, types))
    .map((run) => run.id)
  const selectedIds = [...selectedRunIds]
  const selectedRetryIds = visibleRuns
    .filter((run) => selectedRunIds.has(run.id) && isTaskRetryable(run, types))
    .map((run) => run.id)
  const selectedDeleteIds = visibleRuns
    .filter((run) => selectedRunIds.has(run.id) && run.status !== "running")
    .map((run) => run.id)
  const allVisibleSelected = visibleRuns.length > 0 && visibleRuns.every((run) => selectedRunIds.has(run.id))

  function toggleSelection(id: number, checked: boolean) {
    setSelectedRunIds((current) => {
      const next = new Set(current)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }

  function toggleAllVisible(checked: boolean) {
    setSelectedRunIds((current) => {
      const next = new Set(current)
      visibleRuns.forEach((run) => (checked ? next.add(run.id) : next.delete(run.id)))
      return next
    })
  }

  return (
    <>
      <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:px-5 md:px-6">
        <Select
          value={statusFilter}
          onValueChange={(value) => {
            setStatusFilter(value as StatusFilter)
            setPage(1)
          }}
        >
          <SelectTrigger
            aria-label={t("tasks.filter.status")}
            className="w-full sm:w-48"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">{t("tasks.filter.allStatuses")}</SelectItem>
              <SelectItem value="active">{t("tasks.filter.active")}</SelectItem>
              {taskRunStatuses.map((status) => (
                <SelectItem key={status} value={status}>
                  {t(`tasks.status.${status}`)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select
          value={typeFilter}
          onValueChange={(value) => {
            setTypeFilter(value)
            setPage(1)
          }}
        >
          <SelectTrigger
            aria-label={t("tasks.filter.type")}
            className="w-full sm:w-64"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">{t("tasks.filter.allTypes")}</SelectItem>
              {types.map((type) => (
                <SelectItem key={type.type} value={type.type}>
                  {getTaskTypeLabel(type.type, t)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <div className="text-sm text-muted-foreground sm:ml-auto">
          {t("tasks.total", { count: runsQuery.data?.total ?? 0 })}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:px-5 md:px-6">
        <Button type="button" size="sm" variant="outline" disabled={isBatchPending || failedRunIds.length === 0} onClick={() => batchMutation.mutate({ action: "retry", ids: failedRunIds })}>
          <RotateCcw data-icon="inline-start" />
          {t("tasks.retryFailed")}
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={isBatchPending || selectedRetryIds.length === 0} onClick={() => batchMutation.mutate({ action: "retry", ids: selectedRetryIds })}>
          <RotateCcw data-icon="inline-start" />
          {t("tasks.retrySelected")}
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={isBatchPending || selectedDeleteIds.length === 0} onClick={() => batchMutation.mutate({ action: "delete", ids: selectedDeleteIds })}>
          <Trash2 data-icon="inline-start" />
          {t("tasks.deleteSelected")}
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={isBatchPending || deleteMutation.isPending} onClick={() => clearCompletedMutation.mutate()}>
          {clearCompletedMutation.isPending ? <Spinner data-icon="inline-start" /> : <CheckCheck data-icon="inline-start" />}
          {t("tasks.clearCompleted")}
        </Button>
        <Button type="button" size="sm" variant="ghost" disabled={isBatchPending || selectedIds.length === 0} onClick={() => setSelectedRunIds(new Set())}>
          {t("tasks.clearSelection")}
        </Button>
      </div>

      {actionError ? (
        <Alert className="m-4 md:mx-6" variant="destructive">
          <AlertDescription>{(actionError as Error).message}</AlertDescription>
        </Alert>
      ) : null}

      {runsQuery.isLoading ? <TaskRunsSkeleton /> : null}
      {runsQuery.isError ? (
        <div className="p-4 md:p-6">
          <Alert variant="destructive">
            <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
              <span>{(runsQuery.error as Error).message}</span>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void runsQuery.refetch()}
              >
                {t("state.retry")}
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      {runsQuery.isSuccess && runsQuery.data.items.length === 0 ? (
        <Empty className="min-h-72">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <RotateCcw />
            </EmptyMedia>
            <EmptyTitle>{t("tasks.runs.empty")}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : null}
      {runsQuery.data?.items.length ? (
        <>
          <DesktopTaskRuns
            runs={runsQuery.data.items}
            types={types}
            locale={locale}
            cancelingRunId={
              cancelMutation.isPending ? (cancelMutation.variables ?? null) : null
            }
            retryingRunId={
              retryMutation.isPending ? (retryMutation.variables ?? null) : null
            }
            selectedRunIds={selectedRunIds}
            allVisibleSelected={allVisibleSelected}
            onToggleSelect={toggleSelection}
            onToggleSelectAll={toggleAllVisible}
            onCancel={(id) => cancelMutation.mutate(id)}
            onDelete={openDelete}
            onRetry={(id) => retryMutation.mutate(id)}
            onView={onSelectedRunChange}
          />
          <MobileTaskRuns
            runs={runsQuery.data.items}
            types={types}
            locale={locale}
            cancelingRunId={
              cancelMutation.isPending ? (cancelMutation.variables ?? null) : null
            }
            retryingRunId={
              retryMutation.isPending ? (retryMutation.variables ?? null) : null
            }
            selectedRunIds={selectedRunIds}
            allVisibleSelected={allVisibleSelected}
            onToggleSelect={toggleSelection}
            onToggleSelectAll={toggleAllVisible}
            onCancel={(id) => cancelMutation.mutate(id)}
            onDelete={openDelete}
            onRetry={(id) => retryMutation.mutate(id)}
            onView={onSelectedRunChange}
          />
        </>
      ) : null}

      {runsQuery.isSuccess && runsQuery.data.total > 0 ? (
        <div className="flex min-h-16 items-center justify-between gap-3 border-t px-4 sm:px-5 md:px-6">
          <span className="text-sm text-muted-foreground">
            {t("tasks.page", { page, total: totalPages })}
          </span>
          <div className="flex gap-1">
            <Button
              type="button"
              size="icon-lg"
              variant="ghost"
              aria-label={t("tasks.previousPage")}
              disabled={page <= 1}
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              <ChevronLeft />
            </Button>
            <Button
              type="button"
              size="icon-lg"
              variant="ghost"
              aria-label={t("tasks.nextPage")}
              disabled={page >= totalPages}
              onClick={() =>
                setPage((current) => Math.min(totalPages, current + 1))
              }
            >
              <ChevronRight />
            </Button>
          </div>
        </div>
      ) : null}

      <TaskRunDetailDialog
        runId={selectedRunId}
        types={types}
        onTaskCreated={onTaskCreated}
        onDelete={openDelete}
        onRunChange={onSelectedRunChange}
      />

      <Dialog
        open={deletingRun !== null}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setDeletingRun(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("tasks.deleteRun")}</DialogTitle>
          </DialogHeader>
          <div className="break-words text-sm text-muted-foreground">
            {deletingRun ? getTaskSubject(deletingRun, t) : ""}
          </div>
          {deleteMutation.error ? (
            <Alert variant="destructive">
              <AlertDescription>
                {(deleteMutation.error as Error).message}
              </AlertDescription>
            </Alert>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={deleteMutation.isPending}
              onClick={() => setDeletingRun(null)}
            >
              {t("settings.cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={() => {
                if (deletingRun) deleteMutation.mutate(deletingRun.id)
              }}
            >
              {deleteMutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Trash2 data-icon="inline-start" />
              )}
              {t("files.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function DesktopTaskRuns({
  runs,
  types,
  locale,
  cancelingRunId,
  retryingRunId,
  onCancel,
  onDelete,
  onRetry,
  onView,
  selectedRunIds,
  allVisibleSelected,
  onToggleSelect,
  onToggleSelectAll,
}: TaskRunListProps) {
  const { t } = useI18n()

  return (
    <div className="hidden md:block">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-12 pl-5 md:pl-6">
              <Checkbox
                aria-label={t("tasks.selectAll")}
                checked={allVisibleSelected}
                onCheckedChange={(checked) => onToggleSelectAll(checked === true)}
              />
            </TableHead>
            <TableHead>{t("tasks.subject")}</TableHead>
            <TableHead className="w-44">{t("tasks.status")}</TableHead>
            <TableHead className="w-56">{t("tasks.progress")}</TableHead>
            <TableHead className="w-48">{t("tasks.createdAt")}</TableHead>
            <TableHead className="w-36 pr-5 text-right md:pr-6">
              {t("files.actions")}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => (
            <TableRow key={run.id}>
              <TableCell className="pl-5 md:pl-6">
                <Checkbox
                  aria-label={t("tasks.selectRun", { id: run.id })}
                  checked={selectedRunIds.has(run.id)}
                  onCheckedChange={(checked) => onToggleSelect(run.id, checked === true)}
                />
              </TableCell>
              <TableCell className="max-w-0 px-5 py-4 md:px-6">
                <button
                  type="button"
                  className="block min-h-11 w-full min-w-0 text-left outline-none focus-visible:underline"
                  onClick={() => onView(run.id)}
                >
                  <span className="block truncate font-medium">
                    {getTaskSubject(run, t)}
                  </span>
                  <span className="mt-1 block truncate text-xs text-muted-foreground">
                    {getTaskTypeLabel(run.type, t)}
                  </span>
                </button>
              </TableCell>
              <TableCell>
                <TaskStatusBadge status={run.status} />
              </TableCell>
              <TableCell>
                <TaskProgress run={run} />
              </TableCell>
              <TableCell className="text-muted-foreground">
                {formatDate(run.created_at, locale)}
              </TableCell>
              <TableCell className="pr-5 md:pr-6">
                <TaskRunActions
                  run={run}
                  types={types}
                  cancelingRunId={cancelingRunId}
                  retryingRunId={retryingRunId}
                  onCancel={onCancel}
                  onDelete={onDelete}
                  onRetry={onRetry}
                  onView={onView}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function MobileTaskRuns(props: TaskRunListProps) {
  const { runs, locale, onView, selectedRunIds, onToggleSelect } = props
  const { t } = useI18n()

  return (
    <div className="divide-y md:hidden">
      {runs.map((run) => (
        <article className="space-y-3 p-4" key={run.id}>
          <div className="flex min-w-0 items-start gap-3">
            <Checkbox
              aria-label={t("tasks.selectRun", { id: run.id })}
              checked={selectedRunIds.has(run.id)}
              onCheckedChange={(checked) => onToggleSelect(run.id, checked === true)}
            />
            <button
              type="button"
              className="block min-h-11 min-w-0 flex-1 text-left outline-none focus-visible:underline"
              onClick={() => onView(run.id)}
            >
              <span className="block truncate font-medium">
                {getTaskSubject(run, t)}
              </span>
              <span className="mt-1 block truncate text-xs text-muted-foreground">
                {getTaskTypeLabel(run.type, t)}
              </span>
            </button>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <TaskStatusBadge status={run.status} />
            <span className="text-xs text-muted-foreground">
              {formatDate(run.created_at, locale)}
            </span>
          </div>
          <TaskProgress run={run} />
          <TaskRunActions {...props} run={run} />
        </article>
      ))}
    </div>
  )
}

interface TaskRunListProps {
  runs: TaskRun[]
  types: TaskType[]
  locale: string
  cancelingRunId: number | null
  retryingRunId: number | null
  onCancel: (id: number) => void
  onDelete: (run: TaskRun) => void
  onRetry: (id: number) => void
  onView: (id: number) => void
  selectedRunIds: Set<number>
  allVisibleSelected: boolean
  onToggleSelect: (id: number, checked: boolean) => void
  onToggleSelectAll: (checked: boolean) => void
}

function TaskProgress({ run }: { run: TaskRun }) {
  const { t } = useI18n()
  const isScanning = run.progress.stage === "scanning"
  const showStage = run.progress.stage !== "" && !isScanning && run.progress.percent < 100

  return (
    <div className="min-w-32 space-y-1.5">
      {isScanning || showStage ? (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {run.status === "running" ? <Spinner className="size-3.5 shrink-0" /> : null}
          <span>{getProgressLabel(run, t)}</span>
        </div>
      ) : null}
      <div className="flex items-center gap-3">
        <Progress
          aria-label={`${run.progress.percent}%`}
          className="min-w-16"
          value={run.progress.percent}
        />
        <span className="w-9 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
          {run.progress.percent}%
        </span>
      </div>
    </div>
  )
}

function TaskRunActions({
  run,
  types,
  cancelingRunId,
  retryingRunId,
  onCancel,
  onDelete,
  onRetry,
  onView,
}: Omit<
  TaskRunListProps,
  "runs" | "locale" | "selectedRunIds" | "allVisibleSelected" | "onToggleSelect" | "onToggleSelectAll"
> & { run: TaskRun }) {
  const { t } = useI18n()
  const descriptor = types.find((type) => type.type === run.type)
  const canCancel = isTaskActive(run.status) && descriptor?.cancelable === true
  const canRetry = isTaskRetryable(run, types)
  const canDelete = run.status !== "running"
  const isCanceling = cancelingRunId !== null
  const isRetrying = retryingRunId !== null

  return (
    <div className="flex items-center justify-end gap-1">
      <TaskIconButton
        label={t("tasks.view")}
        icon={<Eye />}
        onClick={() => onView(run.id)}
      />
      {canCancel ? (
        <TaskIconButton
          label={t("tasks.cancel")}
          disabled={isCanceling}
          icon={cancelingRunId === run.id ? <Spinner /> : <Square />}
          onClick={() => onCancel(run.id)}
        />
      ) : null}
      {canRetry ? (
        <TaskIconButton
          label={t("tasks.retry")}
          disabled={isRetrying}
          icon={retryingRunId === run.id ? <Spinner /> : <RotateCcw />}
          onClick={() => onRetry(run.id)}
        />
      ) : null}
      {canDelete ? (
        <TaskIconButton
          label={t("tasks.deleteRun")}
          icon={<Trash2 />}
          onClick={() => onDelete(run)}
        />
      ) : null}
    </div>
  )
}

function TaskIconButton({
  label,
  icon,
  disabled,
  onClick,
}: {
  label: string
  icon: ReactNode
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon-lg"
          variant="ghost"
          aria-label={label}
          disabled={disabled}
          onClick={onClick}
        >
          {icon}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function TaskRunsSkeleton() {
  return (
    <div className="space-y-px">
      {Array.from({ length: 5 }).map((_, index) => (
        <div
          className="grid min-h-18 grid-cols-[minmax(0,1fr)_9rem_12rem] items-center gap-4 border-b px-4 md:px-6"
          key={index}
        >
          <div className="space-y-2">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/3" />
          </div>
          <Skeleton className="h-5 w-20" />
          <Skeleton className="h-2 w-full" />
        </div>
      ))}
    </div>
  )
}
