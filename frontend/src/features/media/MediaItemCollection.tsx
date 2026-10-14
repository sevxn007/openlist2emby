import { type ReactNode, useCallback, useEffect, useRef, useState } from "react"
import {
  ChevronLeft,
  ChevronRight,
  Disc3,
  Film,
  FolderOpen,
  Music2,
  Star,
  Tv,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { MediaLibraryItem, MediaLibraryItemKind } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { cn } from "@/lib/utils"

import {
  isFolderItem,
  mediaGridClassName,
  mediaItemLayout,
  mediaRailClassName,
  MediaItemLayout,
} from "./mediaItemPresentation"

interface MediaItemCollectionProps {
  items: MediaLibraryItem[]
  onOpen: (item: MediaLibraryItem) => void
  showEpisodeSeries?: boolean
  layoutOverride?: MediaItemLayout
  variant?: "grid" | "rail"
  showGroupHeadings?: boolean
  showProgress?: boolean
}

interface MediaRailProps {
  title: string
  items: MediaLibraryItem[]
  onOpen: (item: MediaLibraryItem) => void
  onTitleClick?: () => void
  layout?: MediaItemLayout
  showProgress?: boolean | ((item: MediaLibraryItem) => boolean)
  renderActionMenu?: (item: MediaLibraryItem) => ReactNode
  hasMore?: boolean
  loadingMore?: boolean
  onLoadMore?: () => Promise<boolean>
}

const kindOrder: MediaLibraryItemKind[] = [
  "movie",
  "series",
  "season",
  "episode",
  "music_artist",
  "music_album",
  "audio",
]

export function MediaItemCollection({
  items,
  onOpen,
  layoutOverride,
  showEpisodeSeries = false,
  variant = "grid",
  showGroupHeadings = true,
  showProgress = false,
}: MediaItemCollectionProps) {
  const { t } = useI18n()
  const groups = groupItemsByKind(items)
  const shouldShowGroupHeadings = showGroupHeadings && groups.length > 1

  return (
    <div className="flex min-w-0 flex-col gap-8">
      {groups.map(({ kind, items: groupItems }) => {
        const layout = layoutOverride ?? mediaItemLayout(kind)
        return (
          <section key={kind} className="min-w-0">
            {shouldShowGroupHeadings ? (
              <div className="mb-4 flex items-baseline gap-2 border-b border-border pb-2">
                <h2 className="text-sm font-semibold text-foreground">
                  {t(`mediaLibrary.kind.${kind}`)}
                </h2>
                <span className="text-xs tabular-nums text-muted-foreground">
                  {groupItems.length}
                </span>
              </div>
            ) : null}
            <div
              className={
                variant === "rail"
                  ? mediaRailClassName(layout)
                  : mediaGridClassName(layout)
              }
            >
              {groupItems.map((item) => (
                <MediaItemTile
                  key={item.id}
                  item={item}
                  layout={layout}
                  showEpisodeSeries={showEpisodeSeries}
                  showProgress={showProgress}
                  onOpen={() => onOpen(item)}
                />
              ))}
            </div>
          </section>
        )
      })}
    </div>
  )
}

export function MediaRail({
  title,
  items,
  onOpen,
  onTitleClick,
  layout,
  showProgress = false,
  renderActionMenu,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
}: MediaRailProps) {
  const resolvedLayout = layout ?? (items[0] ? mediaItemLayout(items[0].kind) : "poster")
  const railRef = useRef<HTMLDivElement>(null)
  const pendingScrollRef = useRef(false)
  const loadingRef = useRef(false)
  const [scrollState, setScrollState] = useState({
    canScrollBack: false,
    canScrollForward: false,
  })

  const updateScrollState = useCallback(() => {
    const rail = railRef.current
    if (!rail) return
    const nextState = {
      canScrollBack: rail.scrollLeft > 1,
      canScrollForward: rail.scrollLeft + rail.clientWidth < rail.scrollWidth - 1,
    }
    setScrollState((current) =>
      current.canScrollBack === nextState.canScrollBack &&
      current.canScrollForward === nextState.canScrollForward
        ? current
        : nextState,
    )
  }, [])

  useEffect(() => {
    const rail = railRef.current
    if (!rail) return

    updateScrollState()
    let frame: number | undefined
    if (pendingScrollRef.current) {
      pendingScrollRef.current = false
      frame = requestAnimationFrame(() => {
        rail.scrollBy({ left: Math.max(rail.clientWidth * 0.8, 320), behavior: "smooth" })
      })
    }
    window.addEventListener("resize", updateScrollState)
    const resizeObserver = new ResizeObserver(updateScrollState)
    resizeObserver.observe(rail)
    return () => {
      if (frame !== undefined) cancelAnimationFrame(frame)
      window.removeEventListener("resize", updateScrollState)
      resizeObserver.disconnect()
    }
  }, [items.length, updateScrollState])

  if (items.length === 0) return null

  async function scrollRail(direction: -1 | 1) {
    const rail = railRef.current
    if (!rail) return
    if (direction === 1 && !scrollState.canScrollForward && hasMore && onLoadMore) {
      if (loadingMore || loadingRef.current) return
      loadingRef.current = true
      pendingScrollRef.current = true
      try {
        if (!await onLoadMore()) pendingScrollRef.current = false
      } finally {
        loadingRef.current = false
      }
      return
    }
    rail.scrollBy({
      left: direction * Math.max(rail.clientWidth * 0.8, 320),
      behavior: "smooth",
    })
  }

  return (
    <section className="group/rail min-w-0" aria-label={title}>
      <div className="mb-4 flex items-start gap-3">
        <h2 className="min-w-0 flex-1 text-xl font-semibold text-foreground sm:text-2xl">
          {onTitleClick ? (
            <button
              type="button"
              className="-ml-3 flex min-h-11 max-w-full items-start gap-2 rounded-md px-3 py-2 text-left transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
              onClick={onTitleClick}
            >
              <span className="break-words">{title}</span>
              <ChevronRight className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
            </button>
          ) : (
            title
          )}
        </h2>
        {onTitleClick ? null : <ChevronRight className="size-5 text-muted-foreground" />}
      </div>
      <div className="relative">
        <MediaRailControls
          title={title}
          canScrollBack={scrollState.canScrollBack}
          canScrollForward={scrollState.canScrollForward || hasMore}
          loading={loadingMore}
          onScroll={(direction) => void scrollRail(direction)}
        />

        <div
          ref={railRef}
          className={mediaRailClassName(resolvedLayout)}
          onScroll={updateScrollState}
        >
        {items.map((item) => (
          <MediaItemTile
            key={item.id}
            item={item}
            layout={resolvedLayout}
            showProgress={typeof showProgress === "function" ? showProgress(item) : showProgress}
            actionMenu={renderActionMenu?.(item)}
            onOpen={() => onOpen(item)}
          />
        ))}
        </div>
      </div>
    </section>
  )
}

export function MediaRailControls({ title, canScrollBack, canScrollForward, loading = false, onScroll }: {
  title: string
  canScrollBack: boolean
  canScrollForward: boolean
  loading?: boolean
  onScroll: (direction: -1 | 1) => void
}) {
  const { t } = useI18n()
  return (
    <>
      {canScrollBack ? (
        <div className="pointer-events-none absolute top-0 bottom-0 left-0 z-10 w-24 bg-gradient-to-r from-background via-background/60 to-transparent" />
      ) : null}

      {canScrollBack ? (
        <button
          type="button"
          aria-label={t("mediaLibrary.previousItems", { section: title })}
          className="absolute top-1/2 left-2 z-20 flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-background/95 text-foreground shadow-lg ring-1 ring-border backdrop-blur-sm transition hover:bg-background hover:ring-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:opacity-50"
          onClick={() => onScroll(-1)}
        >
          <ChevronLeft className="size-5" />
        </button>
      ) : null}

      {canScrollForward ? (
        <div className="pointer-events-none absolute top-0 bottom-0 right-0 z-10 w-24 bg-gradient-to-l from-background via-background/60 to-transparent" />
      ) : null}

      {canScrollForward ? (
        <button
          type="button"
          aria-label={t("mediaLibrary.nextItems", { section: title })}
          disabled={loading}
          className="absolute top-1/2 right-2 z-20 flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-background/95 text-foreground shadow-lg ring-1 ring-border backdrop-blur-sm transition hover:bg-background hover:ring-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:opacity-50"
          onClick={() => onScroll(1)}
        >
          <ChevronRight className="size-5" />
        </button>
      ) : null}
    </>
  )
}

function MediaItemTile({
  item,
  layout,
  showProgress,
  showEpisodeSeries = false,
  actionMenu,
  onOpen,
}: {
  item: MediaLibraryItem
  layout: MediaItemLayout
  showProgress: boolean
  showEpisodeSeries?: boolean
  actionMenu?: ReactNode
  onOpen: () => void
}) {
  if (layout === "landscape" && item.kind === "episode") {
    return <EpisodeTile showSeries={showEpisodeSeries} item={item} showProgress={showProgress} actionMenu={actionMenu} onOpen={onOpen} />
  }
  if (layout === "list") {
    return <AudioRow item={item} onOpen={onOpen} />
  }
  return (
      <ArtworkTile
        item={item}
      layout={layout}
      showProgress={showProgress}
      actionMenu={actionMenu}
      onOpen={onOpen}
    />
  )
}

function ArtworkTile({
  item,
  layout,
  showProgress,
  actionMenu,
  onOpen,
}: {
  item: MediaLibraryItem
  layout: "poster" | "landscape" | "square"
  showProgress?: boolean
  actionMenu?: ReactNode
  onOpen: () => void
}) {
  const { t } = useI18n()

  return (
    <div className="group relative min-w-0">
    <button
      type="button"
      aria-label={item.name}
      className="w-full min-w-0 rounded-md text-left transition-transform duration-200 hover:-translate-y-0.5 focus-visible:outline-none"
      onClick={onOpen}
    >
      <MediaArtwork
        item={item}
        orientation={layout === "square" ? "square" : layout === "landscape" ? "landscape" : "portrait"}
        showProgress={showProgress}
      />
      <div className="mt-2 min-w-0">
        <div className="truncate text-sm font-medium text-foreground" title={item.name}>
          {item.name}
        </div>
        <MediaMetadata item={item} fallback={itemSecondaryText(item, t)} />
      </div>
    </button>
      {actionMenu ? <div className="absolute right-2 top-2 z-10">{actionMenu}</div> : null}
    </div>
  )
}

function EpisodeTile({
  item,
  showSeries = false,
  showProgress,
  actionMenu,
  onOpen,
}: {
  item: MediaLibraryItem
  showSeries?: boolean
  showProgress: boolean
  actionMenu?: ReactNode
  onOpen: () => void
}) {
  const { t } = useI18n()
  const title = showSeries
    ? [item.series_name, episodeNumber(item), item.name].filter(Boolean).join(" · ")
    : item.name

  return (
    <div className="group relative min-w-0">
    <button
      type="button"
      aria-label={showSeries ? title : `${episodeNumber(item)} ${title}`}
      className="w-full min-w-0 rounded-md text-left transition-transform duration-200 hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      onClick={onOpen}
    >
      <div className="relative">
        <MediaArtwork item={item} orientation="landscape" showProgress={showProgress} />
        <Badge
          variant="secondary"
          className="absolute bottom-2 left-2 border-0 bg-black/75 font-medium tabular-nums text-white shadow-sm backdrop-blur-sm"
        >
          {episodeNumber(item)}
        </Badge>
      </div>
      <div className="mt-2 min-w-0">
        <div className="line-clamp-2 text-sm leading-5 font-medium text-foreground" title={title}>
          {title}
        </div>
        <MediaMetadata item={item} fallback={t("mediaLibrary.kind.episode")} />
      </div>
    </button>
      {actionMenu ? <div className="absolute right-2 top-2 z-10">{actionMenu}</div> : null}
    </div>
  )
}

function AudioRow({
  item,
  onOpen,
}: {
  item: MediaLibraryItem
  onOpen: () => void
}) {
  const { t } = useI18n()

  return (
    <button
      type="button"
      aria-label={item.name}
      className="group grid min-h-18 w-full grid-cols-[3.5rem_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-1 py-2 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      onClick={onOpen}
    >
      <MediaArtwork item={item} orientation="square" compact />
      <div className="min-w-0">
          <div className="truncate text-sm font-medium text-foreground" title={item.name}>
            {item.name}
          </div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {item.album_artist || item.album || t("mediaLibrary.kind.audio")}
        </div>
      </div>
      {item.track_number ? (
        <span className="pr-2 text-xs tabular-nums text-muted-foreground">
          {String(item.track_number).padStart(2, "0")}
        </span>
      ) : null}
    </button>
  )
}

function MediaArtwork({
  item,
  orientation,
  compact = false,
  showProgress = false,
}: {
  item: MediaLibraryItem
  orientation: "portrait" | "landscape" | "square"
  compact?: boolean
  showProgress?: boolean
}) {
  const { t } = useI18n()
  const Icon = itemIcon(item)
  const folder = isFolderItem(item)
  const progress = playbackProgress(item)

  return (
    <div
      data-media-orientation={orientation}
      className={cn(
        "relative overflow-hidden bg-muted ring-1 ring-border transition group-hover:ring-primary/60 group-focus-visible:ring-3 group-focus-visible:ring-ring/60",
        compact ? "size-14 rounded-md" : "w-full rounded-lg",
        !compact && orientation === "portrait" && "aspect-[2/3]",
        !compact && orientation === "landscape" && "aspect-video",
        !compact && orientation === "square" && "aspect-square",
      )}
    >
      {item.image_url ? (
        <img
          src={item.image_url}
          alt=""
          loading="lazy"
          className="size-full object-cover transition duration-200 group-hover:scale-[1.02]"
        />
      ) : (
        <div className="flex size-full items-center justify-center text-muted-foreground">
          <Icon className={compact ? "size-6" : "size-9"} strokeWidth={1.5} />
        </div>
      )}
      {folder ? (
          <Badge className="absolute right-2 bottom-2 border-0 bg-black/70 text-white shadow-sm" variant="secondary">
            <FolderOpen className="size-3" />
          </Badge>
      ) : null}
      {showProgress && progress !== null ? (
        <div
          role="progressbar"
          aria-label={t("mediaLibrary.watchedProgress", { name: item.name })}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
          className="absolute inset-x-2 bottom-2 h-1.5 overflow-hidden rounded-full bg-black/65 shadow-sm"
        >
          <div
            className="h-full rounded-full bg-primary"
            style={{ width: `${progress}%` }}
          />
        </div>
      ) : null}
    </div>
  )
}

function playbackProgress(item: MediaLibraryItem) {
  const runtime = item.runtime_ticks ?? 0
  const position = item.user_data.playback_position_ticks
  if (runtime <= 0 || position <= 0) return null
  return Math.min(100, Math.max(0, Math.round((position / runtime) * 100)))
}

function MediaMetadata({
  item,
  fallback,
}: {
  item: MediaLibraryItem
  fallback: string
}) {
  const { t } = useI18n()

  return (
    <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      <span className="min-w-0 truncate">{item.production_year || fallback}</span>
      {item.community_rating ? (
        <span
          className="ml-auto flex shrink-0 items-center gap-1 tabular-nums"
          title={t("mediaLibrary.rating", {
            rating: item.community_rating.toFixed(1),
          })}
        >
          <Star className="size-3 fill-current text-primary" />
          {item.community_rating.toFixed(1)}
        </span>
      ) : null}
    </div>
  )
}

function groupItemsByKind(items: MediaLibraryItem[]) {
  const byKind = new Map<MediaLibraryItemKind, MediaLibraryItem[]>()
  for (const item of items) {
    const group = byKind.get(item.kind)
    if (group) {
      group.push(item)
    } else {
      byKind.set(item.kind, [item])
    }
  }
  return kindOrder.flatMap((kind) => {
    const group = byKind.get(kind)
    return group ? [{ kind, items: group }] : []
  })
}

function itemIcon(item: MediaLibraryItem) {
  if (item.kind === "movie") return Film
  if (item.kind === "series" || item.kind === "season" || item.kind === "episode") return Tv
  if (item.kind === "audio") return Music2
  return Disc3
}

function episodeNumber(item: MediaLibraryItem) {
  return `S${String(item.season_number ?? 0).padStart(2, "0")}E${String(item.episode_number ?? 0).padStart(2, "0")}`
}

function itemSecondaryText(
  item: MediaLibraryItem,
  t: ReturnType<typeof useI18n>["t"],
) {
  if (item.kind === "audio") {
    return [item.track_number, item.album_artist].filter(Boolean).join(" · ")
  }
  return t(`mediaLibrary.kind.${item.kind}`)
}
