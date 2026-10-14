import {type ReactNode, useCallback, useEffect, useMemo, useRef, useState} from "react"
import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core"
import {
  arrayMove,
  horizontalListSortingStrategy,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable"
import {CSS} from "@dnd-kit/utilities"
import {useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient,} from "@tanstack/react-query"
import {
  ArrowDownAZ,
  CalendarClock,
  CheckCircle2,
  ChevronRight,
  Edit3,
  Eye,
  Film,
  FolderOpen,
  GripVertical,
  Home,
  LibraryBig,
  ListX,
  MoreHorizontal,
  Music2,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
  Tv,
} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,} from "@/components/ui/dialog"
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,} from "@/components/ui/dropdown-menu"
import {Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle,} from "@/components/ui/empty"
import {Input} from "@/components/ui/input"
import {Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue,} from "@/components/ui/select"
import {Skeleton} from "@/components/ui/skeleton"
import {Spinner} from "@/components/ui/spinner"
import {Tooltip, TooltipContent, TooltipTrigger,} from "@/components/ui/tooltip"
import {
  clearMediaLibraryResume,
  deleteMediaLibrary,
  getMediaLibraryItem,
  getTaskRun,
  listMediaLibraries,
  listMediaLibraryContinueWatching,
  listMediaLibraryItems,
  listTaskSchedules,
  MEDIA_SCAN_TASK_TYPE,
  MediaLibrary,
  type MediaLibraryContinueWatchingItem,
  MediaLibraryItem,
  MediaLibraryItemKind,
  MediaLibraryPage as MediaLibraryPageResponse,
  reorderMediaLibraries,
  scanMediaLibrary,
  setMediaLibraryPlayed,
  setMediaLibraryResumeVisibility,
  TaskRun,
  TaskSchedule,
} from "@/shared/api/manager"
import {useI18n} from "@/shared/i18n/useI18n"
import {getProgressLabel} from "@/features/tasks/taskModel"
import {taskQueryKeys} from "@/features/tasks/queryKeys"
import {formatDate} from "@/shared/lib/format"

import {MediaItemCollection, MediaRail, MediaRailControls} from "./MediaItemCollection"
import {MediaItemDetail} from "./MediaItemDetail"
import {MediaItemMetadataDialog} from "./MediaItemMetadataDialog"
import {MusicMetadataDialog} from "./MusicMetadataDialog"
import {MediaLibraryDialog} from "./MediaLibraryDialog"
import {isFolderItem, mediaGridClassName, MediaItemLayout, mediaLayoutForContext,} from "./mediaItemPresentation"
import {mediaScanSchedulesByLibrary} from "./mediaScanSchedule"

interface MediaLibraryPageProps {
	isAdmin?: boolean
  onTaskCreated: (run: TaskRun) => void
  onViewTask: (runId: number) => void
  onPlay: (item: MediaLibraryItem) => void
}

// Keep the first render small; the browse view fetches subsequent pages on demand.
const MEDIA_PAGE_SIZE = 60

export function MediaLibraryPage({
	isAdmin = true,
  onTaskCreated,
  onViewTask,
  onPlay,
}: MediaLibraryPageProps) {
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const [selectedLibraryId, setSelectedLibraryId] = useState<number | null>(null)
  const [viewMode, setViewMode] = useState<"home" | "browse">("home")
  const [parents, setParents] = useState<MediaLibraryItem[]>([])
  const [search, setSearch] = useState("")
  const [sortBy, setSortBy] = useState("SortName")
  const [sortOrder, setSortOrder] = useState<"ascending" | "descending">("ascending")
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingLibrary, setEditingLibrary] = useState<MediaLibrary | null>(null)
  const [deletingLibrary, setDeletingLibrary] = useState<MediaLibrary | null>(null)
  const [detailItem, setDetailItem] = useState<MediaLibraryItem | null>(null)
  const [metadataItem, setMetadataItem] = useState<MediaLibraryItem | null>(null)
  const [musicEmbedItem, setMusicEmbedItem] = useState<MediaLibraryItem | null>(null)
  const [detailSeasonId, setDetailSeasonId] = useState<number | undefined>()
  const [highlightedEpisodeId, setHighlightedEpisodeId] = useState<number | undefined>()
  const [scanRunId, setScanRunId] = useState<number | null>(null)
  const [loadAllPages, setLoadAllPages] = useState(false)

  useEffect(() => {
    document.documentElement.scrollTop = 0
    document.body.scrollTop = 0
  }, [detailItem])

  const librariesQuery = useQuery({
    queryKey: ["media-libraries"],
    queryFn: listMediaLibraries,
  })
  const libraries = useMemo(
    () => librariesQuery.data?.items ?? [],
    [librariesQuery.data?.items],
  )
  const scanSchedulesQuery = useQuery({
    queryKey: taskQueryKeys.schedules(MEDIA_SCAN_TASK_TYPE),
    queryFn: () => listTaskSchedules(MEDIA_SCAN_TASK_TYPE),
    enabled: isAdmin,
  })
  const scanSchedulesByLibrary = useMemo(
    () => mediaScanSchedulesByLibrary(scanSchedulesQuery.data?.items ?? []),
    [scanSchedulesQuery.data?.items],
  )

  useEffect(() => {
    if (libraries.length === 0) {
      setSelectedLibraryId(null)
      setParents([])
      setViewMode("home")
      return
    }
    if (!selectedLibraryId || !libraries.some((item) => item.id === selectedLibraryId)) {
      setSelectedLibraryId(libraries[0].id)
      setParents([])
      setSearch("")
      setViewMode("home")
    }
  }, [libraries, selectedLibraryId])

  const selectedLibrary = libraries.find((item) => item.id === selectedLibraryId)

  const currentParent = parents[parents.length - 1]
  const normalizedSearch = search.trim()
  const showingSearch = normalizedSearch.length > 0

  const homeQueryConfigs = useMemo(
    () =>
      libraries.map((library) => ({
        queryKey: ["media-library-home", library.id],
        queryFn: () =>
          listMediaLibraryItems({
            libraryId: library.id,
            kinds: latestKinds(library),
            latest: true,
            limit: 8,
          }),
        enabled: viewMode === "home" && !showingSearch,
      })),
    [libraries, showingSearch, viewMode],
  )
  const homeQueries = useQueries({ queries: homeQueryConfigs })
  const resumeQuery = useInfiniteQuery({
    queryKey: ["media-library-continue-watching"],
    queryFn: ({ pageParam }) => listMediaLibraryContinueWatching(pageParam, 24),
    initialPageParam: 0,
    getNextPageParam: (lastPage, pages) => {
      const loaded = pages.reduce((count, page) => count + page.items.length, 0)
      return lastPage.items.length > 0 && loaded < lastPage.total ? loaded : undefined
    },
    enabled: viewMode === "home" && !showingSearch,
  })

  const playedMutation = useMutation({
    mutationFn: ({ id, played }: { id: number; played: boolean }) => setMediaLibraryPlayed(id, played),
    onSuccess: (userData, variables) => {
	  setDetailItem((current) => current?.id === variables.id ? { ...current, user_data: userData } : current)
      void queryClient.invalidateQueries({ queryKey: ["media-library-continue-watching"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-item"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
	  void queryClient.invalidateQueries({ queryKey: ["media-library-detail-episodes"] })
	  void queryClient.invalidateQueries({ queryKey: ["media-library-detail-seasons"] })
    },
  })
  const hideResumeMutation = useMutation({
    mutationFn: (id: number) => setMediaLibraryResumeVisibility(id, true),
    onSuccess: (userData, id) => {
      setDetailItem((current) => current?.id === id ? { ...current, user_data: userData } : current)
      void queryClient.invalidateQueries({ queryKey: ["media-library-continue-watching"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-item"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
    },
    onError: () => toast.error(t("mediaLibrary.removeFromContinueWatchingFailed")),
  })

  const clearResumeMutation = useMutation({
    mutationFn: (id: number) => clearMediaLibraryResume(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["media-library-continue-watching"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-item"] })
    },
  })

  const itemsQuery = useInfiniteQuery({
    queryKey: [
      "media-library-items",
      viewMode,
      selectedLibraryId ?? "all",
      currentParent?.id ?? "root",
      normalizedSearch,
      sortBy,
      sortOrder,
    ],
    queryFn: ({ pageParam }) => {
      const musicRoot =
        viewMode === "browse" &&
        selectedLibrary?.content_type === "music" &&
        !currentParent &&
        !showingSearch
      return listMediaLibraryItems({
        libraryId: viewMode === "browse" ? selectedLibraryId ?? undefined : undefined,
        parentId:
          viewMode === "browse" && !showingSearch && !musicRoot
            ? currentParent?.id ?? "root"
            : undefined,
        kinds: musicRoot ? ["audio"] : undefined,
        search: normalizedSearch || undefined,
        recursive: showingSearch,
        sortBy,
        sortOrder,
        start: pageParam,
        limit: MEDIA_PAGE_SIZE,
      })
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage, pages) => {
      const loadedItems = pages.reduce(
        (total, page) => total + page.items.length,
        0,
      )
      if (lastPage.items.length === 0 || loadedItems >= lastPage.total) {
        return undefined
      }
      return loadedItems
    },
    enabled: (viewMode === "browse" && selectedLibraryId !== null) || showingSearch,
  })

  const scanQuery = useQuery({
    queryKey: ["media-library-scan", scanRunId],
    queryFn: () => getTaskRun(scanRunId ?? 0),
    enabled: isAdmin && scanRunId !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === "queued" || status === "running" ? 1_000 : false
    },
  })

  useEffect(() => {
    const run = scanQuery.data
    if (
      !run ||
      !["succeeded", "failed", "canceled"].includes(run.status)
    ) {
      return
    }
    if (run.status === "succeeded") {
      toast.success(t("mediaLibrary.scanCompleted"))
      void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-resume"] })
    } else {
      toast.error(t("mediaLibrary.scanFailed"))
    }
    setScanRunId(null)
  }, [queryClient, scanQuery.data, t])

  const scanMutation = useMutation({
    mutationFn: () => scanMediaLibrary(selectedLibraryId ?? 0),
    onSuccess: (run) => {
      onTaskCreated(run)
      setScanRunId(run.id)
      toast.success(t("mediaLibrary.scanStarted"))
    },
    onError: () => toast.error(t("mediaLibrary.scanFailed")),
  })

  const reorderMutation = useMutation({
    mutationFn: reorderMediaLibraries,
    onMutate: async (libraryIDs) => {
      await queryClient.cancelQueries({ queryKey: ["media-libraries"] })
      const previous = queryClient.getQueryData<MediaLibraryPageResponse>([
        "media-libraries",
      ])
      queryClient.setQueryData<MediaLibraryPageResponse>(
        ["media-libraries"],
        (current) =>
          current
            ? { ...current, items: librariesInOrder(current.items, libraryIDs) }
            : current,
      )
      return { previous }
    },
    onError: (_error, _libraryIDs, context) => {
      if (context?.previous) {
        queryClient.setQueryData(["media-libraries"], context.previous)
      }
      toast.error(t("mediaLibrary.reorderFailed"))
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (library: MediaLibrary) => deleteMediaLibrary(library.id),
    onSuccess: () => {
      setDeletingLibrary(null)
      setParents([])
      setSearch("")
      closeDetail()
      setViewMode("home")
      void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
      toast.success(t("mediaLibrary.deleted"))
    },
  })

  const scanActive =
    scanMutation.isPending ||
    scanQuery.data?.status === "queued" ||
    scanQuery.data?.status === "running"

  const sortedItems = useMemo(
    () => itemsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [itemsQuery.data?.pages],
  )
  const {
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isSuccess: itemsQuerySucceeded,
  } = itemsQuery

  useEffect(() => {
    if (!loadAllPages || viewMode !== "browse" || !itemsQuerySucceeded) return
    if (hasNextPage && !isFetchingNextPage) {
      void fetchNextPage()
      return
    }
    if (!hasNextPage) setLoadAllPages(false)
  }, [
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    itemsQuerySucceeded,
    loadAllPages,
    viewMode,
  ])

  function openLibrary(library: MediaLibrary, loadAll = false) {
    setSelectedLibraryId(library.id)
    setParents([])
    setSearch("")
    closeDetail()
    setLoadAllPages(loadAll)
    setViewMode("browse")
  }

  function openItem(item: MediaLibraryItem) {
    if (item.kind === "series" || item.kind === "season") {
      showDetail(item)
      return
    }
    if (isFolderItem(item)) {
      setParents((current) => {
        if (!showingSearch) return [...current, item]
        const parent = current[current.length - 1]
        return item.parent_id === parent?.id ? [...current, item] : [item]
      })
      setSearch("")
      setViewMode("browse")
      return
    }
    showDetail(item)
  }

  function showDetail(
    item: MediaLibraryItem,
    target: { seasonId?: number; episodeId?: number } = {},
  ) {
    setDetailItem(item)
    setDetailSeasonId(target.seasonId)
    setHighlightedEpisodeId(target.episodeId)
  }

  function closeDetail() {
    setDetailItem(null)
    setDetailSeasonId(undefined)
    setHighlightedEpisodeId(undefined)
  }

  function handleMetadataSaved(updated: MediaLibraryItem) {
    setMetadataItem((current) => (current?.id === updated.id ? updated : current))
    setDetailItem((current) => (current?.id === updated.id ? updated : current))
    setParents((current) =>
      current.map((parent) => (parent.id === updated.id ? updated : parent)),
    )
  }

  async function openResumeItem(item: MediaLibraryItem) {
    const seasonId = item.kind === "episode" ? item.parent_id : undefined
    if (!seasonId) {
      openItem(item)
      return
    }

    try {
      const season = await queryClient.fetchQuery({
        queryKey: ["media-library-item", seasonId],
        queryFn: () => getMediaLibraryItem(seasonId),
      })
      const seriesId = season.kind === "season" ? season.parent_id : undefined
      if (!seriesId) {
        openItem(item)
        return
      }
      const series = await queryClient.fetchQuery({
        queryKey: ["media-library-item", seriesId],
        queryFn: () => getMediaLibraryItem(seriesId),
      })
      if (series.kind !== "series") {
        openItem(item)
        return
      }

      setSelectedLibraryId(series.library_id)
      setParents([])
      setSearch("")
      setLoadAllPages(false)
      setViewMode("browse")
      showDetail(series, { seasonId: season.id, episodeId: item.id })
    } catch {
      openItem(item)
    }
  }

  function openAddDialog() {
    setEditingLibrary(null)
    setDialogOpen(true)
  }

  if (librariesQuery.isLoading) {
    return <MediaLibrarySkeleton />
  }

  if (librariesQuery.isError) {
    return (
      <div className="media-library-surface rounded-none bg-background text-foreground">
        <Empty className="min-h-80 border-border bg-transparent text-foreground">
          <EmptyHeader>
            <EmptyMedia variant="icon"><LibraryBig /></EmptyMedia>
            <EmptyTitle>{t("mediaLibrary.loadFailed")}</EmptyTitle>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" onClick={() => void librariesQuery.refetch()}>
              <RefreshCw data-icon="inline-start" />
              {t("state.retry")}
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  const homePreviewItems = new Map(
    libraries.map((library, index) => [
      library.id,
      {
        items: homeQueries[index]?.data?.items ?? [],
        total: homeQueries[index]?.data?.total ?? 0,
      },
    ]),
  )

  return (
    <div className="media-library-surface min-h-[calc(100svh-4rem)] min-w-0 bg-background text-foreground">
      {!detailItem ? (
      <div className="sticky top-14 z-20 flex flex-col gap-4 border-b border-border bg-background/95 px-5 py-4 backdrop-blur-md md:top-0 lg:px-8">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          {!detailItem && !(viewMode === "home" && !showingSearch) ? (
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t("mediaLibrary.home")}
                className="text-muted-foreground hover:bg-muted hover:text-accent-foreground"
                onClick={() => {
                  closeDetail()
                  setParents([])
                  setSearch("")
                  setViewMode("home")
                }}
              >
                <Home />
              </Button>
              <h1 className="truncate text-xl font-semibold text-foreground sm:text-2xl">
                {showingSearch ? t("mediaLibrary.searchResults") : selectedLibrary?.name}
              </h1>
            </div>
          ) : <div className="min-w-0 flex-1" />}

          <div className="relative order-last w-full sm:order-none sm:w-64 lg:w-80">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => {
                setSearch(event.target.value)
                if (event.target.value.trim()) closeDetail()
              }}
              placeholder={t("mediaLibrary.search")}
              className="h-10 border-border bg-muted/50 pl-9 text-foreground placeholder:text-muted-foreground focus-visible:border-ring/60 focus-visible:ring-ring/20"
            />
          </div>

          {!detailItem && isAdmin ? (
            <>
              {viewMode === "browse" && selectedLibrary ? (
                <Button
                  type="button"
                  disabled={scanActive}
                  aria-label={t("mediaLibrary.scan")}
                  className="h-10 rounded-full bg-primary px-4 text-sm text-primary-foreground hover:bg-primary/90"
                  onClick={() => scanMutation.mutate()}
                >
                  {scanActive ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}
                  <span className="hidden sm:inline">{t("mediaLibrary.scan")}</span>
                </Button>
              ) : null}
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={t("mediaLibrary.addLibrary")}
                className="border-border bg-muted/50 text-foreground hover:bg-muted hover:text-accent-foreground"
                onClick={openAddDialog}
              >
                <Plus />
              </Button>
              {viewMode === "browse" && selectedLibrary ? (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="outline"
                      size="icon-sm"
                      aria-label={t("mediaLibrary.libraryActions")}
                      className="border-border bg-muted/50 text-foreground hover:bg-muted hover:text-accent-foreground"
                    >
                      <MoreHorizontal />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      onSelect={() => {
                        setEditingLibrary(selectedLibrary)
                        setDialogOpen(true)
                      }}
                    >
                      <Edit3 />
                      {t("mediaLibrary.editLibrary")}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      variant="destructive"
                      onSelect={() => setDeletingLibrary(selectedLibrary)}
                    >
                      <Trash2 />
                      {t("mediaLibrary.deleteLibrary")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </>
          ) : null}
        </div>

        {viewMode === "browse" && !detailItem && selectedLibrary ? (
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <Select
              value={String(selectedLibrary.id)}
              onValueChange={(value) => {
                setSelectedLibraryId(Number(value))
                setParents([])
                setSearch("")
              }}
            >
              <SelectTrigger className="h-9 w-full max-w-60 border-border bg-muted/50 text-foreground">
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="border-border bg-muted text-foreground">
                <SelectGroup>
                  {libraries.map((library) => (
                    <SelectItem key={library.id} value={String(library.id)}>
                      {library.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Select value={sortBy} onValueChange={setSortBy}>
              <SelectTrigger className="h-9 w-full max-w-48 border-border bg-muted/50 text-foreground" aria-label={t("mediaLibrary.sortBy")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="border-border bg-muted text-foreground">
                <SelectItem value="SortName">{t("mediaLibrary.sortNameOption")}</SelectItem>
                <SelectItem value="PremiereDate">{t("mediaLibrary.releaseDateOption")}</SelectItem>
                <SelectItem value="LatestEpisodeDate">{t("mediaLibrary.latestEpisodeDateOption")}</SelectItem>
                <SelectItem value="ProductionYear">{t("mediaLibrary.productionYearOption")}</SelectItem>
                <SelectItem value="CommunityRating">{t("mediaLibrary.ratingOption")}</SelectItem>
                <SelectItem value="DateCreated">{t("mediaLibrary.addedDateOption")}</SelectItem>
              </SelectContent>
            </Select>
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              aria-label={sortOrder === "ascending" ? t("mediaLibrary.sortDescending") : t("mediaLibrary.sortAscending")}
              title={sortOrder === "ascending" ? t("mediaLibrary.sortDescending") : t("mediaLibrary.sortAscending")}
              className="shrink-0 border-border bg-muted/50 text-foreground hover:bg-muted"
              onClick={() => setSortOrder((current) => current === "ascending" ? "descending" : "ascending")}
            >
              <ArrowDownAZ className={sortOrder === "descending" ? "rotate-180" : undefined} />
            </Button>
            {isAdmin ? (
              <MediaScanScheduleStatus
                schedule={scanSchedulesByLibrary.get(selectedLibrary.id)}
                locale={locale}
              />
            ) : null}
            <MediaBreadcrumb
              library={selectedLibrary}
              parents={parents}
              onNavigate={(count) => {
                setParents((current) => current.slice(0, count))
                setSearch("")
              }}
            />
              {currentParent && isAdmin ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t("mediaLibrary.editMetadata")}
                className="shrink-0 text-muted-foreground hover:bg-muted hover:text-accent-foreground"
                onClick={() => setMetadataItem(currentParent)}
              >
                <Edit3 />
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
      ) : null}

      {isAdmin && scanActive && scanQuery.data ? (
        <button
          type="button"
          className="flex min-h-11 w-full items-center gap-2 border-b border-border px-5 py-3 text-left text-sm text-muted-foreground lg:px-8"
          onClick={() => onViewTask(scanQuery.data.id)}
        >
          <Spinner className="text-primary" />
          <span className="min-w-0 flex-1 truncate">
            {getProgressLabel(scanQuery.data, t)}
          </span>
          <ChevronRight className="size-4" />
        </button>
      ) : null}

      <main className="min-w-0 px-5 py-7 lg:px-8 lg:py-9">
        {detailItem ? (
          <MediaItemDetail
            key={`${detailItem.id}-${detailSeasonId ?? "default"}`}
            item={detailItem}
            canManage={isAdmin}
            initialSeasonId={detailSeasonId}
            highlightedEpisodeId={highlightedEpisodeId}
            onBack={closeDetail}
            onEditMetadata={setMetadataItem}
            onEmbedMusicMetadata={isAdmin ? setMusicEmbedItem : undefined}
            onPlay={onPlay}
			onSetPlayed={(item, played) => playedMutation.mutate({ id: item.id, played })}
				onOpenItem={(item) => void openResumeItem(item)}
          />
        ) : showingSearch || viewMode === "browse" ? (
          <BrowseView
            library={selectedLibrary}
            parents={parents}
            items={sortedItems}
            query={itemsQuery}
            canManage={isAdmin}
            scanActive={scanActive}
            search={showingSearch}
            onOpen={openItem}
            onScan={() => scanMutation.mutate()}
          />
        ) : (
          <HomeView
            libraries={libraries}
            scanSchedules={scanSchedulesByLibrary}
            previewItems={homePreviewItems}
            continueItems={resumeQuery.data?.pages.flatMap((page) => page.items) ?? []}
            hasMoreContinue={resumeQuery.hasNextPage}
            loadingMoreContinue={resumeQuery.isFetchingNextPage}
            onLoadMoreContinue={async () => {
              const result = await resumeQuery.fetchNextPage()
              if (result.isFetchNextPageError) toast.error(t("mediaLibrary.loadFailed"))
              return !result.isFetchNextPageError
            }}
            loading={homeQueries.some((query) => query.isLoading)}
			canManage={isAdmin}
			isReordering={reorderMutation.isPending}
            onOpenLibrary={openLibrary}
            onOpenItem={openItem}
            onOpenResumeItem={openResumeItem}
			onPlay={onPlay}
			onMarkPlayed={(item, played) => playedMutation.mutate({ id: item.id, played })}
			onClearResume={async (item) => {
			  await clearResumeMutation.mutateAsync(item.id)
			}}
			onHideResume={(item) => hideResumeMutation.mutate(item.id)}
            hidingResume={hideResumeMutation.isPending}
			onReorder={(libraryIDs) => reorderMutation.mutate(libraryIDs)}
          />
        )}
      </main>

      {isAdmin ? (
        <>
          <MediaLibraryDialog
            library={editingLibrary}
            open={dialogOpen}
            onClose={() => setDialogOpen(false)}
            onSaved={(library) => {
              setSelectedLibraryId(library.id)
              setParents([])
              setSearch("")
              closeDetail()
              setViewMode("browse")
              void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
              void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
              void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
            }}
          />
          <Dialog
            open={Boolean(deletingLibrary)}
            onOpenChange={(open) => !open && setDeletingLibrary(null)}
          >
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{t("mediaLibrary.deleteLibrary")}</DialogTitle>
              </DialogHeader>
              <p className="text-sm text-muted-foreground">
                {t("mediaLibrary.deleteConfirmation", {
                  name: deletingLibrary?.name ?? "",
                })}
              </p>
              <DialogFooter>
                <Button variant="outline" onClick={() => setDeletingLibrary(null)}>
                  {t("settings.cancel")}
                </Button>
                <Button
                  variant="destructive"
                  disabled={deleteMutation.isPending}
                  onClick={() => deletingLibrary && deleteMutation.mutate(deletingLibrary)}
                >
                  {deleteMutation.isPending ? <Spinner data-icon="inline-start" /> : <Trash2 data-icon="inline-start" />}
                  {t("mediaLibrary.deleteLibrary")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      ) : null}
      <MediaItemMetadataDialog
        item={metadataItem}
        open={metadataItem !== null}
        onClose={() => setMetadataItem(null)}
        onSaved={handleMetadataSaved}
      />
      <MusicMetadataDialog
        open={musicEmbedItem !== null}
        target={musicEmbedItem}
        onClose={() => setMusicEmbedItem(null)}
        onTaskCreated={onTaskCreated}
        onViewTask={onViewTask}
      />
    </div>
  )
}

function MediaScanScheduleStatus({
  schedule,
  locale,
}: {
  schedule?: TaskSchedule
  locale: string
}) {
  const { t } = useI18n()
  if (!schedule) return null
  return (
    <span className="flex min-h-9 max-w-full items-center gap-2 rounded-md border border-border bg-muted/50 px-3 text-xs text-muted-foreground">
      <CalendarClock className="size-4 shrink-0" />
      <span className="truncate">
        {schedule.enabled && schedule.next_run_at
          ? t("mediaLibrary.scanScheduleNext", {
              date: formatDate(schedule.next_run_at, locale),
            })
          : t("mediaLibrary.scanScheduleDisabled")}
      </span>
    </span>
  )
}

function HomeView({
  libraries,
  scanSchedules,
  previewItems,
  continueItems,
  hasMoreContinue,
  loadingMoreContinue,
  onLoadMoreContinue,
  loading,
	canManage,
	isReordering,
  onOpenLibrary,
  onOpenItem,
  onOpenResumeItem,
	onPlay,
	onMarkPlayed,
	onClearResume,
	onHideResume,
	hidingResume,
	onReorder,
}: {
  libraries: MediaLibrary[]
  scanSchedules: Map<number, TaskSchedule>
  previewItems: Map<number, { items: MediaLibraryItem[]; total: number }>
  continueItems: MediaLibraryContinueWatchingItem[]
  hasMoreContinue: boolean
  loadingMoreContinue: boolean
  onLoadMoreContinue: () => Promise<boolean>
  loading: boolean
	canManage: boolean
	isReordering: boolean
  onOpenLibrary: (library: MediaLibrary, loadAll?: boolean) => void
  onOpenItem: (item: MediaLibraryItem) => void
  onOpenResumeItem: (item: MediaLibraryItem) => void
	onPlay: (item: MediaLibraryItem) => void
	onMarkPlayed: (item: MediaLibraryItem, played: boolean) => void
	onClearResume: (item: MediaLibraryItem) => Promise<void>
	onHideResume: (item: MediaLibraryItem) => void
	hidingResume: boolean
	onReorder: (libraryIDs: number[]) => void
}) {
  const { t } = useI18n()
	const libraryShelfRef = useRef<HTMLDivElement>(null)
	const [libraryScrollState, setLibraryScrollState] = useState({
		canScrollBack: false,
		canScrollForward: false,
	})
	const sensors = useSensors(
		useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
		useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
	)
	const updateLibraryScrollState = useCallback(() => {
		const shelf = libraryShelfRef.current
		if (!shelf) return
		const nextState = {
			canScrollBack: shelf.scrollLeft > 1,
			canScrollForward: shelf.scrollLeft + shelf.clientWidth < shelf.scrollWidth - 1,
		}
		setLibraryScrollState((current) =>
			current.canScrollBack === nextState.canScrollBack &&
			current.canScrollForward === nextState.canScrollForward
				? current
				: nextState,
		)
	}, [])

	useEffect(() => {
		const shelf = libraryShelfRef.current
		if (!shelf) return

		updateLibraryScrollState()
		window.addEventListener("resize", updateLibraryScrollState)
		const resizeObserver = new ResizeObserver(updateLibraryScrollState)
		resizeObserver.observe(shelf)
		return () => {
			window.removeEventListener("resize", updateLibraryScrollState)
			resizeObserver.disconnect()
		}
	}, [libraries.length, updateLibraryScrollState])

	function scrollLibraries(direction: -1 | 1) {
		const shelf = libraryShelfRef.current
		if (!shelf) return
		shelf.scrollBy({
			left: direction * Math.max(shelf.clientWidth * 0.8, 272),
			behavior: "smooth",
		})
	}

	function handleDragEnd(event: DragEndEvent) {
		const { active, over } = event
		if (!over || active.id === over.id) return
		const oldIndex = libraries.findIndex((library) => library.id === active.id)
		const newIndex = libraries.findIndex((library) => library.id === over.id)
		if (oldIndex < 0 || newIndex < 0) return
		onReorder(arrayMove(libraries, oldIndex, newIndex).map((library) => library.id))
	}

  if (libraries.length === 0) {
    return (
      <Empty className="min-h-80 border-border bg-transparent text-foreground">
        <EmptyHeader>
          <EmptyMedia variant="icon"><LibraryBig /></EmptyMedia>
          <EmptyTitle>{t("mediaLibrary.noLibraries")}</EmptyTitle>
          <EmptyDescription className="text-muted-foreground">
            {t("mediaLibrary.addFirstLibrary")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex min-w-0 flex-col gap-11">
      <section className="min-w-0" aria-labelledby="media-library-shelf-title">
        <div className="mb-4 flex items-center gap-3">
          <h1 id="media-library-shelf-title" className="min-w-0 truncate text-xl font-semibold text-foreground sm:text-2xl">
            {t("mediaLibrary.myMedia")}
          </h1>
          <ChevronRight className="size-5 text-muted-foreground" />

        </div>
        <div className="relative">
          <MediaRailControls
            title={t("mediaLibrary.myMedia")}
            canScrollBack={libraryScrollState.canScrollBack}
            canScrollForward={libraryScrollState.canScrollForward}
            onScroll={scrollLibraries}
          />
		{canManage ? (
			<DndContext
				collisionDetection={closestCenter}
				sensors={sensors}
				onDragEnd={handleDragEnd}
			>
				<SortableContext
					items={libraries.map((library) => library.id)}
					strategy={horizontalListSortingStrategy}
				>
					<div
						ref={libraryShelfRef}
						data-library-shelf
						className="flex snap-x snap-mandatory gap-5 overflow-x-auto scroll-smooth pb-2 scrollbar-none"
						onScroll={updateLibraryScrollState}
					>
						{libraries.map((library) => (
							<SortableLibraryCard
								key={library.id}
								library={library}
								schedule={scanSchedules.get(library.id)}
								items={previewItems.get(library.id)?.items ?? []}
								disabled={isReordering}
								onOpen={() => onOpenLibrary(library)}
							/>
						))}
					</div>
				</SortableContext>
			</DndContext>
		) : (
			<div
				ref={libraryShelfRef}
				data-library-shelf
				className="flex snap-x snap-mandatory gap-5 overflow-x-auto scroll-smooth pb-2 scrollbar-none"
				onScroll={updateLibraryScrollState}
			>
				{libraries.map((library) => (
					<LibraryCard
						key={library.id}
						library={library}
						schedule={scanSchedules.get(library.id)}
						items={previewItems.get(library.id)?.items ?? []}
						onOpen={() => onOpenLibrary(library)}
					/>
				))}
			</div>
		)}
        </div>
      </section>

      {continueItems.length > 0 ? (
        <MediaRail
          title={t("mediaLibrary.continueWatching")}
          items={continueItems.map((entry) => entry.item)}
          hasMore={hasMoreContinue}
          loadingMore={loadingMoreContinue}
          onLoadMore={onLoadMoreContinue}
          layout="landscape"
          showProgress={(item) => continueItems.find((entry) => entry.item.id === item.id)?.source === "resume"}
          onOpen={onPlay}
		  renderActionMenu={(item) => {
			const entry = continueItems.find((candidate) => candidate.item.id === item.id)
			return <ContinueWatchingMenu item={item} source={entry?.source ?? "resume"} onPlay={onPlay} onOpenDetails={onOpenResumeItem} onMarkPlayed={onMarkPlayed} onClearResume={onClearResume} onHideResume={onHideResume} hidingResume={hidingResume} />
		  }}
        />
      ) : null}

      {loading ? (
        <div className="flex min-w-0 gap-4 overflow-hidden">
          {Array.from({ length: 6 }).map((_, index) => (
            <div key={index} className="w-[10.5rem] shrink-0 space-y-3">
              <Skeleton className="aspect-[2/3] w-full bg-muted" />
              <Skeleton className="h-4 w-4/5 bg-muted" />
            </div>
          ))}
        </div>
      ) : null}

      {libraries.map((library) => {
        const preview = previewItems.get(library.id)
        const items = preview?.items ?? []
        return items.length > 0 ? (
          <div key={library.id} className="flex min-w-0 flex-col gap-4">
            <HomeLibraryRail
              library={library}
              preview={{ items, total: preview?.total ?? items.length }}
              onOpen={onOpenItem}
              onTitleClick={() => onOpenLibrary(library, true)}
            />
          </div>
        ) : null
      })}
    </div>
  )
}

function HomeLibraryRail({
  library,
  preview,
  onOpen,
  onTitleClick,
}: {
  library: MediaLibrary
  preview: { items: MediaLibraryItem[]; total: number }
  onOpen: (item: MediaLibraryItem) => void
  onTitleClick: () => void
}) {
  const { t } = useI18n()
  const query = useInfiniteQuery({
    queryKey: ["media-library-home", library.id, "rail"],
    initialPageParam: 0,
    initialData: { pages: [preview], pageParams: [0] },
    staleTime: 30_000,
    queryFn: ({ pageParam }) => listMediaLibraryItems({
      libraryId: library.id,
      kinds: latestKinds(library),
      latest: true,
      start: pageParam,
      limit: 8,
    }),
    getNextPageParam: (lastPage, pages) => {
      const loaded = pages.reduce((count, page) => count + page.items.length, 0)
      return lastPage.items.length > 0 && loaded < lastPage.total ? loaded : undefined
    },
  })
  return (
    <MediaRail
      title={library.name}
      items={query.data.pages.flatMap((page) => page.items)}
      layout={library.content_type === "music" ? "list" : "poster"}
      onOpen={onOpen}
      onTitleClick={onTitleClick}
      hasMore={query.hasNextPage}
      loadingMore={query.isFetchingNextPage}
      onLoadMore={async () => {
        const result = await query.fetchNextPage()
        if (result.isFetchNextPageError) toast.error(t("mediaLibrary.loadFailed"))
        return !result.isFetchNextPageError
      }}
    />
  )
}

function ContinueWatchingMenu({
	item,
	source,
	onPlay,
	onOpenDetails,
	onMarkPlayed,
	onClearResume,
	onHideResume,
	hidingResume,
}: {
	item: MediaLibraryItem
	source: "resume" | "next_up"
	onPlay: (item: MediaLibraryItem) => void
	onOpenDetails: (item: MediaLibraryItem) => void
	onMarkPlayed: (item: MediaLibraryItem, played: boolean) => void
	onClearResume: (item: MediaLibraryItem) => Promise<void>
	onHideResume: (item: MediaLibraryItem) => void
	hidingResume: boolean
}) {
	const { t } = useI18n()
	const restart = async () => {
		if (source === "resume") await onClearResume(item)
		onPlay({
			...item,
			user_data: { ...item.user_data, playback_position_ticks: 0 },
		})
	}

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button
					type="button"
					variant="secondary"
					size="icon"
					className="size-11 border border-border bg-black/75 text-white shadow-md hover:bg-black"
					aria-label={t("mediaLibrary.continueWatchingActions", { name: item.name })}
				>
					<MoreHorizontal />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="w-52 max-w-[calc(100vw-2rem)] [&_[role=menuitem]]:min-h-11">
				<DropdownMenuItem onSelect={() => onPlay(item)}>
					<Play />
					{source === "resume" ? t("mediaLibrary.resume") : t("mediaLibrary.play")}
				</DropdownMenuItem>
				<DropdownMenuItem onSelect={() => void restart()}>
					<RotateCcw />
					{t("mediaLibrary.restart")}
				</DropdownMenuItem>
				<DropdownMenuItem onSelect={() => onOpenDetails(item)}>
					<Eye />
					{t("mediaLibrary.viewDetails")}
				</DropdownMenuItem>
				<DropdownMenuItem onSelect={() => onMarkPlayed(item, true)}>
					<CheckCircle2 />
					{t("mediaLibrary.markPlayed")}
				</DropdownMenuItem>
				<DropdownMenuItem disabled={hidingResume} onSelect={() => onHideResume(item)}>
					<ListX />
					{t("mediaLibrary.removeFromContinueWatching")}
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	)
}

function LibraryCard({
  library,
  schedule,
  items,
  onOpen,
	dragHandle,
}: {
  library: MediaLibrary
  schedule?: TaskSchedule
  items: MediaLibraryItem[]
  onOpen: () => void
	dragHandle?: ReactNode
}) {
  const { locale, t } = useI18n()
  const Icon = library.content_type === "music" ? Music2 : library.content_type === "tvshows" ? Tv : Film
  const slots = Array.from({ length: 4 }, (_, index) => items[index])
  return (
	<div className="group relative w-[17rem] shrink-0 snap-start sm:w-[19rem]">
		<button
			type="button"
			aria-label={library.name}
			className="w-full text-left transition-transform duration-200 hover:-translate-y-0.5 focus-visible:outline-none"
			onClick={onOpen}
		>
			<div className="relative grid aspect-[16/8] grid-cols-4 overflow-hidden rounded-md bg-muted ring-1 ring-border transition group-hover:ring-primary/60 group-focus-visible:ring-2 group-focus-visible:ring-ring/60">
					{slots.map((item, index) => (
						<div key={item ? `item-${item.id}` : `empty-${index}`} className="relative min-w-0 overflow-hidden border-r border-black/20 last:border-r-0">
						{item?.image_url ? (
							<img src={item.image_url} alt="" loading="lazy" className="size-full object-cover transition duration-200 group-hover:scale-[1.03]" />
						) : (
							<div className="flex size-full items-center justify-center text-muted-foreground">
								<Icon className="size-7" strokeWidth={1.3} />
							</div>
						)}
					</div>
				))}
				<div className="pointer-events-none absolute inset-x-0 bottom-0 h-1/2 bg-gradient-to-t from-black/65 to-transparent" />
			</div>
			<div className="mt-3 flex items-center gap-2">
				<span className="truncate text-base font-medium text-foreground">{library.name}</span>
				<span className="shrink-0 text-xs text-muted-foreground">{t(`mediaLibrary.type.${library.content_type}`)}</span>
			</div>
			{schedule ? (
				<div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
					<CalendarClock className="size-3.5 shrink-0" />
					<span className="truncate">
						{schedule.enabled && schedule.next_run_at
							? t("mediaLibrary.scanScheduleNext", { date: formatDate(schedule.next_run_at, locale) })
							: t("mediaLibrary.scanScheduleDisabled")}
					</span>
				</div>
			) : null}
		</button>
		{dragHandle}
	</div>
  )
}

function SortableLibraryCard({
	library,
	schedule,
	items,
	disabled,
	onOpen,
}: {
	library: MediaLibrary
	schedule?: TaskSchedule
	items: MediaLibraryItem[]
	disabled: boolean
	onOpen: () => void
}) {
	const { t } = useI18n()
	const {
		attributes,
		isDragging,
		listeners,
		setActivatorNodeRef,
		setNodeRef,
		transform,
		transition,
	} = useSortable({ id: library.id, disabled })
	return (
		<div
			ref={setNodeRef}
			style={{ transform: CSS.Transform.toString(transform), transition }}
			className={isDragging ? "z-10 shrink-0 opacity-60" : "shrink-0"}
		>
			<LibraryCard
				library={library}
				schedule={schedule}
				items={items}
				onOpen={onOpen}
				dragHandle={
					<Tooltip>
						<TooltipTrigger asChild>
							<Button
								ref={setActivatorNodeRef}
								type="button"
								variant="ghost"
								size="icon-sm"
								aria-label={t("mediaLibrary.reorder")}
								className="absolute top-2 right-2 z-10 bg-black/45 text-white/75 backdrop-blur-sm hover:bg-black/70 hover:text-white"
								{...attributes}
								{...listeners}
							>
								<GripVertical />
							</Button>
						</TooltipTrigger>
						<TooltipContent side="top">{t("mediaLibrary.reorder")}</TooltipContent>
					</Tooltip>
				}
			/>
		</div>
	)
}

function librariesInOrder(libraries: MediaLibrary[], libraryIDs: number[]) {
	const byID = new Map(libraries.map((library) => [library.id, library]))
	const ordered = libraryIDs.flatMap((id) => {
		const library = byID.get(id)
		return library ? [library] : []
	})
	return ordered.length === libraries.length ? ordered : libraries
}

function BrowseView({
  library,
  parents,
  items,
  query,
  canManage,
  scanActive,
  search,
  onOpen,
  onScan,
}: {
  library?: MediaLibrary
  parents: MediaLibraryItem[]
  items: MediaLibraryItem[]
  query: {
    isLoading: boolean
    isError: boolean
    isSuccess: boolean
    hasNextPage?: boolean
    isFetchingNextPage: boolean
    fetchNextPage: () => Promise<unknown>
  }
  canManage: boolean
  scanActive: boolean
  search: boolean
  onOpen: (item: MediaLibraryItem) => void
  onScan: () => void
}) {
  const { t } = useI18n()
  const currentParent = parents[parents.length - 1]
  const layout = library ? mediaLayoutForContext(library, currentParent) : "poster"

  if (!library && !search) {
    return (
      <Empty className="min-h-72 border-border bg-transparent text-foreground">
        <EmptyHeader>
          <EmptyMedia variant="icon"><LibraryBig /></EmptyMedia>
          <EmptyTitle>{t("mediaLibrary.noLibraries")}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  if (query.isLoading) return <MediaGridSkeleton layout={layout} />
  if (query.isError) {
    return (
      <Empty className="min-h-64 border-border bg-transparent text-foreground">
        <EmptyHeader>
          <EmptyMedia variant="icon"><LibraryBig /></EmptyMedia>
          <EmptyTitle>{t("mediaLibrary.itemsFailed")}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }
  if (query.isSuccess && items.length === 0) {
    return (
      <Empty className="min-h-64 border-border bg-transparent text-foreground">
        <EmptyHeader>
          <EmptyMedia variant="icon"><FolderOpen /></EmptyMedia>
          <EmptyTitle>{t("mediaLibrary.empty")}</EmptyTitle>
          {!search ? <EmptyDescription className="text-muted-foreground">{t("mediaLibrary.noItems")}</EmptyDescription> : null}
        </EmptyHeader>
        {canManage && !search && library?.last_scanned_at === undefined ? (
          <EmptyContent>
            <Button disabled={scanActive} className="rounded-full bg-primary text-primary-foreground hover:bg-primary/90" onClick={onScan}>
              <RefreshCw data-icon="inline-start" />
              {t("mediaLibrary.scan")}
            </Button>
          </EmptyContent>
        ) : null}
      </Empty>
    )
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <MediaItemCollection items={items} onOpen={onOpen} />
      {query.hasNextPage ? (
        <Button
          variant="outline"
          className="self-center border-border bg-muted/50 text-foreground hover:bg-muted hover:text-accent-foreground"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage ? <Spinner data-icon="inline-start" /> : <ChevronRight data-icon="inline-start" />}
          {t("mediaLibrary.loadMore")}
        </Button>
      ) : null}
    </div>
  )
}

function MediaBreadcrumb({
  library,
  parents,
  onNavigate,
}: {
  library: MediaLibrary
  parents: MediaLibraryItem[]
  onNavigate: (count: number) => void
}) {
  const { t } = useI18n()
  return (
    <nav
      className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto scrollbar-none"
      aria-label={t("common.breadcrumb")}
    >
      <Button
        variant="ghost"
        size="sm"
        className="shrink-0 px-2 text-muted-foreground hover:bg-muted hover:text-accent-foreground"
        onClick={() => onNavigate(0)}
      >
        {library.name}
      </Button>
      {parents.map((item, index) => (
        <span key={item.id} className="flex min-w-0 items-center gap-1">
          <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
          <Button
            variant="ghost"
            size="sm"
            className="max-w-48 min-w-0 px-2 text-muted-foreground hover:bg-muted hover:text-accent-foreground"
            onClick={() => onNavigate(index + 1)}
          >
            <span className="truncate">{item.name}</span>
          </Button>
        </span>
      ))}
    </nav>
  )
}

function MediaLibrarySkeleton() {
  return (
    <div className="media-library-surface min-h-[calc(100svh-4rem)] bg-background px-5 py-7 lg:px-8">
      <div className="mb-9 flex items-center justify-between gap-4">
        <Skeleton className="h-8 w-44 bg-muted" />
        <Skeleton className="h-10 w-56 bg-muted" />
      </div>
      <div className="mb-11 flex gap-5 overflow-hidden">
        {Array.from({ length: 3 }).map((_, index) => (
          <div key={index} className="w-[19rem] shrink-0 space-y-3">
            <Skeleton className="aspect-[16/8] w-full bg-muted" />
            <Skeleton className="h-4 w-3/5 bg-muted" />
          </div>
        ))}
      </div>
      <MediaGridSkeleton layout="poster" />
    </div>
  )
}

function MediaGridSkeleton({ layout }: { layout: MediaItemLayout }) {
  const skeletons = layout === "list" ? 5 : layout === "landscape" ? 6 : 8
  return (
    <div className={mediaGridClassName(layout)}>
      {Array.from({ length: skeletons }).map((_, index) =>
        layout === "list" ? (
          <div key={index} className="flex min-h-18 items-center gap-3 py-2">
            <Skeleton className="size-14 shrink-0 bg-muted" />
            <div className="min-w-0 flex-1 space-y-2">
              <Skeleton className="h-4 w-2/5 bg-muted" />
              <Skeleton className="h-3 w-1/4 bg-muted" />
            </div>
          </div>
        ) : (
          <div key={index} className="space-y-2">
            <Skeleton
              className={
                layout === "landscape"
                  ? "aspect-video w-full bg-muted"
                  : layout === "square"
                    ? "aspect-square w-full bg-muted"
                    : "aspect-[2/3] w-full bg-muted"
              }
            />
            <Skeleton className="h-4 w-4/5 bg-muted" />
            <Skeleton className="h-3 w-2/5 bg-muted" />
          </div>
        ),
      )}
    </div>
  )
}

function latestKinds(library: MediaLibrary): MediaLibraryItemKind[] {
  if (library.content_type === "tvshows") return ["series"]
  if (library.content_type === "music") return ["audio"]
  return ["movie"]
}
