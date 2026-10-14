import { FormEvent, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  CircleCheck,
  ImageOff,
  ListMusic,
  ListTodo,
  Search,
  TriangleAlert,
} from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { MusicSearchResults } from "@/features/media/MusicSearchResults"
import { cn } from "@/lib/utils"
import {
  createTaskRun,
  getMusicDetail,
  getTaskRun,
  joinOpenListPath,
  MUSIC_EMBED_TASK_TYPE,
  MusicTrack,
  OpenListObject,
  searchMusic,
  type TaskRun,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { taskQueryKeys } from "@/features/tasks/queryKeys"
import {
  getProgressLabel,
  getTaskPollingInterval,
  isTaskActive,
} from "@/features/tasks/taskModel"
import { getInitialMusicQuery } from "./musicMetadata"

interface MusicMetadataDialogProps {
  currentPath: string
  open: boolean
  target: OpenListObject | null
  onClose: () => void
  onTaskCreated?: (run: TaskRun) => void
  onViewTask?: (runId: number) => void
}

function getErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

export function MusicMetadataDialog({
  currentPath,
  open,
  target,
  onClose,
  onTaskCreated = () => undefined,
  onViewTask = () => undefined,
}: MusicMetadataDialogProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [queryInput, setQueryInput] = useState("")
  const [submittedQuery, setSubmittedQuery] = useState("")
  const [selectedTrack, setSelectedTrack] = useState<MusicTrack | null>(null)
  const [embedCover, setEmbedCover] = useState(true)
  const [embedLyrics, setEmbedLyrics] = useState(true)
  const [taskId, setTaskId] = useState<number | null>(null)

  const searchQuery = useQuery({
    queryKey: ["music-search", submittedQuery],
    queryFn: () => searchMusic(submittedQuery),
    enabled: open && submittedQuery !== "",
    staleTime: (query) => query.state.data?.results.length ? 5 * 60 * 1000 : 0,
  })

  const detailQuery = useQuery({
    queryKey: [
      "music-detail",
      selectedTrack?.picture_id,
      selectedTrack?.lyric_id,
    ],
    queryFn: () =>
      getMusicDetail(
        "netease",
        selectedTrack?.picture_id ?? "",
        selectedTrack?.lyric_id ?? "",
      ),
    enabled: open && selectedTrack !== null,
    staleTime: 5 * 60 * 1000,
  })

  const embedMutation = useMutation({
    mutationFn: async () => {
      if (!target || !selectedTrack || !detailQuery.data) {
        throw new Error(t("music.selectionRequired"))
      }
      const hasCover = Boolean(detailQuery.data.cover_url)
      const hasLyrics = Boolean(
        detailQuery.data.lyrics?.trim() ||
          detailQuery.data.translated_lyrics?.trim(),
      )
      return createTaskRun({
        type: MUSIC_EMBED_TASK_TYPE,
        payload: {
          path: joinOpenListPath(currentPath, target.name),
          source: selectedTrack.source,
          picture_id: selectedTrack.picture_id,
          lyric_id: selectedTrack.lyric_id,
          embed_cover: embedCover && hasCover,
          embed_lyrics: embedLyrics && hasLyrics,
        },
      })
    },
    onSuccess: (run) => {
      setTaskId(run.id)
      queryClient.setQueryData(taskQueryKeys.run(run.id), run)
      onTaskCreated(run)
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
    },
  })

  const taskQuery = useQuery({
    queryKey: taskQueryKeys.run(taskId ?? 0),
    queryFn: () => getTaskRun(taskId ?? 0),
    enabled: taskId !== null,
    refetchInterval: (query) =>
      getTaskPollingInterval(isTaskActive(query.state.data?.status)),
  })
  const taskRun = taskQuery.data ?? embedMutation.data
  const taskActive = isTaskActive(taskRun?.status)
  const resetEmbedMutation = embedMutation.reset

  useEffect(() => {
    if (!open || !target) {
      return
    }
    setQueryInput(getInitialMusicQuery(target.name))
    setSubmittedQuery("")
    setSelectedTrack(null)
    setEmbedCover(true)
    setEmbedLyrics(true)
    setTaskId(null)
    resetEmbedMutation()
  }, [open, resetEmbedMutation, target])

  const tracks = searchQuery.data?.results ?? []
  const hasCover = Boolean(detailQuery.data?.cover_url)
  const hasLyrics = Boolean(
    detailQuery.data?.lyrics?.trim() ||
      detailQuery.data?.translated_lyrics?.trim(),
  )
  const canEmbed =
    selectedTrack !== null &&
    detailQuery.isSuccess &&
    ((embedCover && hasCover) || (embedLyrics && hasLyrics)) &&
    !taskActive

  function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (taskActive) {
      return
    }
    const nextQuery = queryInput.trim()
    if (!nextQuery) {
      return
    }
    setSelectedTrack(null)
    setTaskId(null)
    embedMutation.reset()
    if (nextQuery === submittedQuery) {
      void searchQuery.refetch()
      return
    }
    setSubmittedQuery(nextQuery)
  }

  function selectTrack(track: MusicTrack) {
    if (taskActive) {
      return
    }
    setSelectedTrack(track)
    setEmbedCover(true)
    setEmbedLyrics(true)
    setTaskId(null)
    embedMutation.reset()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose()
        }
      }}
    >
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader className="px-4 pt-4 sm:px-6 sm:pt-6">
          <DialogTitle>{t("music.title")}</DialogTitle>
          <DialogDescription className="truncate">
            {target?.name}
          </DialogDescription>
        </DialogHeader>

        <DialogBody className="overscroll-contain px-4 pb-4 sm:px-6">
          <form
            className="flex gap-2"
            onSubmit={handleSearch}
          >
            <Input
              className="min-w-0 flex-1"
              aria-label={t("music.searchQuery")}
              value={queryInput}
              onChange={(event) => setQueryInput(event.target.value)}
              placeholder={t("music.searchPlaceholder")}
              disabled={taskActive}
            />
            <Button
              type="submit"
              disabled={!queryInput.trim() || searchQuery.isFetching || taskActive}
            >
              {searchQuery.isFetching ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Search data-icon="inline-start" />
              )}
              {t("music.search")}
            </Button>
          </form>

          {searchQuery.isError ? (
            <MusicError
              className="mt-4"
              message={getErrorMessage(
                searchQuery.error,
                t("music.searchFailed"),
              )}
            />
          ) : null}

          {searchQuery.isLoading ? <MusicSearchSkeleton /> : null}

          {searchQuery.isSuccess && tracks.length === 0 ? (
            <div className="mt-4 flex min-h-28 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
              {t("music.noResults")}
            </div>
          ) : null}

          <div className="mt-4">
            <MusicSearchResults
              disabled={taskActive}
              selectedTrack={selectedTrack}
              tracks={tracks}
              onSelect={selectTrack}
            />
          </div>

          {selectedTrack ? (
            <MusicDetailPanel
              detail={detailQuery.data}
              disabled={taskActive}
              embedCover={embedCover}
              embedLyrics={embedLyrics}
              error={detailQuery.error}
              isLoading={detailQuery.isLoading}
              track={selectedTrack}
              onEmbedCoverChange={setEmbedCover}
              onEmbedLyricsChange={setEmbedLyrics}
            />
          ) : null}

          {taskRun ? (
            <div className="mt-4 space-y-2 border-t pt-4" aria-live="polite">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="font-medium">
                  {getProgressLabel(taskRun, t)}
                </span>
                <span className="tabular-nums text-muted-foreground">
                  {taskRun.progress.percent}%
                </span>
              </div>
              <Progress
                aria-label={t("music.embedProgress")}
                value={taskRun.progress.percent}
              />
            </div>
          ) : null}

          {taskRun?.status === "succeeded" ? (
            <Alert className="mt-4 border-emerald-500/20 text-emerald-700 dark:text-emerald-300">
              <CircleCheck />
              <AlertDescription>{t("music.embedSuccess")}</AlertDescription>
            </Alert>
          ) : null}

          {taskRun?.error || taskQuery.isError || embedMutation.isError ? (
            <MusicError
              className="mt-4"
              message={
                taskRun?.error?.message ??
                getErrorMessage(
                  taskQuery.error ?? embedMutation.error,
                  t("music.embedFailed"),
                )
              }
            />
          ) : null}
        </DialogBody>

        <DialogFooter className="mx-0 mb-0 rounded-none px-4 sm:px-6">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
          >
            {t("tasks.close")}
          </Button>
          {taskRun ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onClose()
                onViewTask(taskRun.id)
              }}
            >
              <ListTodo data-icon="inline-start" />
              {t("music.viewTask")}
            </Button>
          ) : null}
          <Button
            type="button"
            disabled={!canEmbed || embedMutation.isPending || taskActive}
            onClick={() => embedMutation.mutate()}
          >
            {embedMutation.isPending || taskActive ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <ListMusic data-icon="inline-start" />
            )}
            {taskActive ? t("music.taskRunning") : t("music.embed")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MusicDetailPanel({
  detail,
  disabled,
  embedCover,
  embedLyrics,
  error,
  isLoading,
  track,
  onEmbedCoverChange,
  onEmbedLyricsChange,
}: {
  detail: Awaited<ReturnType<typeof getMusicDetail>> | undefined
  disabled: boolean
  embedCover: boolean
  embedLyrics: boolean
  error: Error | null
  isLoading: boolean
  track: MusicTrack
  onEmbedCoverChange: (checked: boolean) => void
  onEmbedLyricsChange: (checked: boolean) => void
}) {
  const { t } = useI18n()
  const [coverFailed, setCoverFailed] = useState(false)

  useEffect(() => {
    setCoverFailed(false)
  }, [detail?.cover_url])

  const lyrics = useMemo(
    () => detail?.lyrics?.trim() || detail?.translated_lyrics?.trim() || "",
    [detail?.lyrics, detail?.translated_lyrics],
  )
  const hasCover = Boolean(detail?.cover_url)
  const hasLyrics = lyrics !== ""

  if (isLoading) {
    return (
      <div className="mt-4 grid gap-4 border-t pt-4 sm:grid-cols-[12rem_minmax(0,1fr)]">
        <Skeleton className="aspect-square w-full rounded-lg" />
        <div className="space-y-3">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-24 w-full" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <MusicError
        className="mt-4"
        message={getErrorMessage(error, t("music.detailFailed"))}
      />
    )
  }

  if (!detail) {
    return null
  }

  return (
    <div className="mt-4 grid gap-4 border-t pt-4 sm:grid-cols-[12rem_minmax(0,1fr)]">
      <div className="space-y-3">
        <div className="flex aspect-square w-full items-center justify-center overflow-hidden rounded-lg bg-muted ring-1 ring-border">
          {hasCover && !coverFailed ? (
            <img
              alt={t("music.coverFor", { title: track.name })}
              className="size-full object-cover"
              src={detail.cover_url}
              onError={() => setCoverFailed(true)}
            />
          ) : (
            <ImageOff className="size-8 text-muted-foreground" />
          )}
        </div>
        <Badge className="max-w-full truncate" variant="secondary">
          {track.artists.join(" / ") || t("music.unknownArtist")}
        </Badge>
      </div>

      <div className="min-w-0 space-y-3">
        <div className="flex min-h-11 flex-wrap items-center gap-x-5 gap-y-2">
          <label
            className={cn(
              "flex min-h-11 cursor-pointer items-center gap-2",
              disabled && "cursor-not-allowed",
            )}
          >
            <Checkbox
              checked={hasCover && embedCover}
              disabled={!hasCover || disabled}
              onCheckedChange={(checked) => onEmbedCoverChange(checked === true)}
            />
            <span>{t("music.cover")}</span>
          </label>
          <label
            className={cn(
              "flex min-h-11 cursor-pointer items-center gap-2",
              disabled && "cursor-not-allowed",
            )}
          >
            <Checkbox
              checked={hasLyrics && embedLyrics}
              disabled={!hasLyrics || disabled}
              onCheckedChange={(checked) => onEmbedLyricsChange(checked === true)}
            />
            <span>{t("music.lyrics")}</span>
          </label>
        </div>
        <div className="max-h-56 min-h-32 overflow-y-auto rounded-lg border bg-muted/30 p-3">
          {hasLyrics ? (
            <pre className="whitespace-pre-wrap break-words font-sans text-xs leading-6">
              {lyrics}
            </pre>
          ) : (
            <div className="flex min-h-24 items-center justify-center text-sm text-muted-foreground">
              {t("music.lyricsUnavailable")}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function MusicSearchSkeleton() {
  return (
    <div className="mt-4 grid gap-2 sm:grid-cols-2">
      {Array.from({ length: 4 }).map((_, index) => (
        <div className="flex min-h-20 items-center gap-3 rounded-lg border p-3" key={index}>
          <Skeleton className="size-11 shrink-0 rounded-lg" />
          <div className="min-w-0 flex-1 space-y-2">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  )
}

function MusicError({ className, message }: { className?: string; message: string }) {
  return (
    <Alert className={className} variant="destructive">
      <TriangleAlert />
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}
