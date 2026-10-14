import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Ban, RefreshCw, Server } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { cancelOpenListTask, listOpenListTasks, type OpenListTask } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import type { TranslationKey } from "@/shared/i18n/messages"
import { formatDate } from "@/shared/lib/format"
import { MarqueeText } from "./MarqueeText"
import { taskQueryKeys } from "./queryKeys"

const kindKeys: Record<OpenListTask["kind"], TranslationKey> = {
  upload: "tasks.openlist.type.upload",
  copy: "tasks.openlist.type.copy",
  move: "tasks.openlist.type.move",
  offline_download: "tasks.openlist.type.offlineDownload",
  offline_download_transfer: "tasks.openlist.type.offlineDownloadTransfer",
  decompress: "tasks.openlist.type.decompress",
  decompress_upload: "tasks.openlist.type.decompressUpload",
}

function statusKey(state: number): TranslationKey {
  if (state === 0) return "tasks.openlist.status.pending"
  if (state === 3) return "tasks.openlist.status.canceling"
  if (state === 8 || state === 9) return "tasks.openlist.status.retrying"
  return "tasks.openlist.status.running"
}

export function OpenListTasksPanel() {
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: taskQueryKeys.openList(),
    queryFn: listOpenListTasks,
    refetchInterval: (current) => (current.state.data?.items.length ? 2_000 : false),
  })
  const cancelMutation = useMutation({
    mutationFn: cancelOpenListTask,
    onSuccess: () => {
      toast.success(t("tasks.openlist.cancelSuccess"))
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.openList() })
    },
  })

  return (
    <section className="border-b">
      <div className="flex min-h-16 items-center gap-3 border-b px-4 py-3 sm:px-5 md:px-6">
        <Server className="size-5 shrink-0 text-primary" />
        <h2 className="text-base font-semibold">{t("tasks.openlist.title")}</h2>
        <Button type="button" size="icon" variant="ghost" className="ml-auto" aria-label={t("tasks.refresh")} onClick={() => void query.refetch()} disabled={query.isFetching}>
          {query.isFetching ? <Spinner /> : <RefreshCw />}
        </Button>
      </div>
      {query.isError ? <Alert className="m-4 md:mx-6" variant="destructive"><AlertDescription>{(query.error as Error).message}</AlertDescription></Alert> : null}
      {query.isLoading ? <div className="p-6"><Spinner /></div> : null}
      {query.isSuccess && query.data.items.length === 0 ? <Empty className="min-h-40"><EmptyHeader><EmptyMedia variant="icon"><Server /></EmptyMedia><EmptyTitle>{t("tasks.openlist.empty")}</EmptyTitle></EmptyHeader></Empty> : null}
      {query.data?.items.length ? (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-72 max-w-72">{t("tasks.name")}</TableHead>
                <TableHead>{t("tasks.type")}</TableHead>
                <TableHead>{t("tasks.status")}</TableHead>
                <TableHead>{t("tasks.progress")}</TableHead>
                <TableHead>{t("tasks.startedAt")}</TableHead>
                <TableHead className="text-right">{t("files.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.items.map((task) => {
                const canCancel = task.state !== 3
                const name = task.name || task.id
                return (
                  <TableRow key={`${task.kind}:${task.id}`}>
                    <TableCell className="w-72 max-w-72 font-medium">
                      <MarqueeText className="w-72" text={name} />
                      {task.error ? (
                        <div className="mt-1 truncate text-xs text-destructive" title={task.error}>
                          {task.error}
                        </div>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{t(kindKeys[task.kind])}</Badge>
                    </TableCell>
                    <TableCell>{t(statusKey(task.state))}</TableCell>
                    <TableCell className="min-w-44">
                      <div className="flex items-center gap-2">
                        <Progress value={task.progress} />
                        <span className="w-10 text-right text-xs text-muted-foreground">
                          {Math.round(task.progress)}%
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                      {task.start_time ? formatDate(task.start_time, locale) : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      {canCancel ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          disabled={cancelMutation.isPending}
                          onClick={() => cancelMutation.mutate(task)}
                        >
                          <Ban data-icon="inline-start" />
                          {t("tasks.cancel")}
                        </Button>
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          {t("tasks.openlist.canceling")}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </section>
  )
}
