import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowLeft,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Circle,
  Clock3,
  Edit3,
  Film,
	FolderOpen,
	HardDrive,
	Music2,
  Play,
  Star,
  Tv,
	UserRound,
	UsersRound,
} from "lucide-react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"

import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
	listMediaLibraryItemPeople,
  listMediaLibraryItems,
  MediaLibraryItem,
	MediaLibraryPerson,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { cn } from "@/lib/utils"

import { MediaItemCollection } from "./MediaItemCollection"
import { displayLyrics } from "./mediaItemPresentation"
import { formatBytes, formatDate as formatTimestamp } from "@/shared/lib/format"

interface MediaItemDetailProps {
  item: MediaLibraryItem
  canManage?: boolean
  initialSeasonId?: number
  highlightedEpisodeId?: number
  onBack: () => void
  onEditMetadata?: (item: MediaLibraryItem) => void
  onEmbedMusicMetadata?: (item: MediaLibraryItem) => void
  onPlay: (item: MediaLibraryItem) => void
	onSetPlayed?: (item: MediaLibraryItem, played: boolean) => void
	onOpenItem: (item: MediaLibraryItem) => void
}

export function MediaItemDetail({
  item,
  canManage = false,
  initialSeasonId,
  highlightedEpisodeId,
  onBack,
  onEditMetadata,
  onEmbedMusicMetadata,
  onPlay,
	onSetPlayed,
	onOpenItem,
}: MediaItemDetailProps) {
  const { locale, t } = useI18n()
  const isSeries = item.kind === "series"
  const isSeason = item.kind === "season"
  const isPlayable = !["series", "season", "music_artist", "music_album"].includes(item.kind)
  const [selectedSeasonId, setSelectedSeasonId] = useState<number | null>(
    initialSeasonId ?? (isSeason ? item.id : null),
  )
	const [selectedEpisodeId, setSelectedEpisodeId] = useState<number | null>(
		highlightedEpisodeId ?? null,
	)
	const [selectedPerson, setSelectedPerson] = useState<MediaLibraryPerson | null>(null)

  useEffect(() => {
    setSelectedSeasonId(initialSeasonId ?? (isSeason ? item.id : null))
  }, [initialSeasonId, isSeason, item.id])

  const seasonsQuery = useQuery({
    queryKey: ["media-library-detail-seasons", item.id],
    queryFn: () =>
      listMediaLibraryItems({
        libraryId: item.library_id,
        parentId: item.id,
        limit: 100,
      }),
    enabled: isSeries,
  })
  const seasons = useMemo(
    () =>
      [...(seasonsQuery.data?.items ?? [])].sort(
        (left, right) => (left.season_number ?? 0) - (right.season_number ?? 0),
      ),
    [seasonsQuery.data?.items],
  )

  useEffect(() => {
    if (!isSeries || selectedSeasonId !== null || seasons.length === 0) return
    setSelectedSeasonId(seasons[0].id)
  }, [isSeries, seasons, selectedSeasonId])

  const episodesQuery = useQuery({
    queryKey: ["media-library-detail-episodes", item.library_id, selectedSeasonId],
    queryFn: () =>
      listMediaLibraryItems({
        libraryId: item.library_id,
        parentId: selectedSeasonId ?? undefined,
        limit: 500,
      }),
    enabled: selectedSeasonId !== null,
  })
  const episodes = useMemo(
    () =>
      [...(episodesQuery.data?.items ?? [])].sort(
        (left, right) => (left.episode_number ?? 0) - (right.episode_number ?? 0),
      ),
    [episodesQuery.data?.items],
  )
  const selectedSeason = seasons.find((season) => season.id === selectedSeasonId)
	const selectedEpisode = episodes.find((episode) => episode.id === selectedEpisodeId)
	const isAudio = item.kind === "audio"
	const peopleTarget = selectedEpisode ?? (
		["movie", "series", "season", "episode"].includes(item.kind) ? item : undefined
	)

	useEffect(() => {
		setSelectedEpisodeId(highlightedEpisodeId ?? null)
		setSelectedPerson(null)
	}, [highlightedEpisodeId, item.id, selectedSeasonId])

	const peopleQuery = useQuery({
		queryKey: ["media-library-item-people", peopleTarget?.id, locale],
		queryFn: () => listMediaLibraryItemPeople(peopleTarget?.id ?? 0, locale),
		enabled: peopleTarget !== undefined && selectedPerson === null,
	})
	const personItemsQuery = useInfiniteQuery({
		queryKey: ["media-library-person-items", selectedPerson?.id],
		initialPageParam: 0,
		queryFn: ({ pageParam }) => listMediaLibraryItems({
			personId: selectedPerson?.id,
			kinds: ["movie", "series", "episode"],
			start: pageParam,
			limit: 100,
		}),
		getNextPageParam: (lastPage, pages) => {
			const loaded = pages.reduce((count, page) => count + page.items.length, 0)
			return lastPage.items.length > 0 && loaded < lastPage.total ? loaded : undefined
		},
		enabled: selectedPerson !== null,
	})
	const personItems = personItemsQuery.data?.pages.flatMap((page) => page.items) ?? []
  const playTarget = isPlayable
    ? item
    : episodes.find((episode) => !episode.user_data.played) ?? episodes[0]
  const cover = item.image_url || item.backdrop_url

  return (
    <section
      data-media-detail
      className="relative isolate -mx-5 -my-7 flex min-w-0 flex-col gap-10 overflow-hidden bg-background px-5 py-7 text-foreground lg:-mx-8 lg:-my-9 lg:px-8 lg:py-9"
    >
      {item.backdrop_url ? (
        <div
          aria-hidden="true"
          data-media-backdrop
          className="pointer-events-none absolute inset-0 z-0 opacity-20"
          style={{
            backgroundImage: `linear-gradient(to bottom, color-mix(in srgb, var(--background) 20%, transparent), var(--background) 82%), url(${item.backdrop_url})`,
            backgroundPosition: "center top",
            backgroundSize: "cover",
          }}
        />
      ) : null}

      <div className="relative z-10 flex items-center justify-between">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="-ml-3 gap-2 text-muted-foreground hover:bg-muted hover:text-accent-foreground"
          onClick={onBack}
        >
          <ArrowLeft data-icon="inline-start" />
          {t("mediaLibrary.backToMedia")}
        </Button>
        <div className="flex items-center gap-1">
          {isAudio && canManage && onEmbedMusicMetadata ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="text-muted-foreground hover:bg-muted hover:text-accent-foreground"
              onClick={() => onEmbedMusicMetadata(item)}
            >
              <Music2 data-icon="inline-start" />
              {t("music.embed")}
            </Button>
          ) : null}
          {(item.kind === "episode" || item.kind === "audio") && canManage && onEditMetadata ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("mediaLibrary.editMetadata")}
              className="text-muted-foreground hover:bg-muted hover:text-accent-foreground"
              onClick={() => onEditMetadata(item)}
            >
              <Edit3 />
            </Button>
          ) : null}
        </div>
      </div>

      <div className="relative z-10 grid min-w-0 gap-8 lg:grid-cols-[minmax(14rem,20rem)_minmax(0,1fr)] lg:gap-12 xl:grid-cols-[20rem_minmax(0,1fr)]">
        <div className="mx-auto w-full max-w-[20rem] lg:mx-0">
          <div
            data-media-orientation={item.kind === "episode" ? "landscape" : isAudio ? "square" : "portrait"}
            className={cn(
              "relative overflow-hidden rounded-md bg-muted shadow-2xl shadow-foreground/10",
              item.kind === "episode" ? "aspect-video" : isAudio ? "aspect-square" : "aspect-[2/3]",
            )}
          >
            {cover ? (
              <img src={cover} alt={item.name} className="size-full object-cover" />
            ) : (
              <div className="flex size-full items-center justify-center text-muted-foreground">
                {item.kind === "episode" ? (
                  <Tv className="size-14" strokeWidth={1.2} />
                ) : (
                  <Film className="size-14" strokeWidth={1.2} />
                )}
              </div>
            )}
          </div>
        </div>

        <div className="flex min-w-0 max-w-4xl flex-col justify-center gap-5">
          <div className="space-y-3">
            <p className="text-xs font-medium uppercase tracking-[0.18em] text-primary">
              {t(`mediaLibrary.kind.${item.kind}`)}
            </p>
            <h1 className="text-3xl font-semibold tracking-tight text-foreground sm:text-4xl">
              {item.name}
            </h1>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
              {item.community_rating ? (
                <span className="inline-flex items-center gap-1.5 text-primary">
                  <Star className="size-4 fill-current" />
                  {item.community_rating.toFixed(1)}
                </span>
              ) : null}
              {item.production_year ? <span>{item.production_year}</span> : null}
              {item.runtime_ticks ? (
                <span className="inline-flex items-center gap-1.5">
                  <Clock3 className="size-3.5" />
                  {formatRuntime(item.runtime_ticks)}
                </span>
              ) : null}
              {item.genres?.slice(0, 3).map((genre) => (
                <span key={genre}>{genre}</span>
              ))}
            </div>
          </div>

		  {isAudio ? <MusicMetadata item={item} /> : null}

          <div className="flex flex-wrap items-center gap-3">
            {playTarget ? (
              <Button
                type="button"
                className="h-11 rounded-full bg-primary px-5 text-primary-foreground hover:bg-primary/90"
                onClick={() => onPlay(playTarget)}
              >
                <Play data-icon="inline-start" className="fill-current" />
                {t("mediaLibrary.play")}
              </Button>
            ) : null}
			{item.user_data.played && onSetPlayed ? (
			  <Button
				type="button"
				variant="outline"
				className="h-11 rounded-full border-border bg-transparent px-5 text-foreground hover:bg-muted"
				onClick={() => onSetPlayed(item, false)}
			  >
				<Circle data-icon="inline-start" />
				{t("mediaLibrary.markUnplayed")}
			  </Button>
			) : null}
            {item.user_data.playback_position_ticks > 0 ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
                  <span
                    className="block h-full bg-primary"
                    style={{
                      width: playbackProgress(item),
                    }}
                  />
                </span>
                {t("mediaLibrary.inProgress")}
              </div>
            ) : null}
          </div>

		  {isAudio ? null : item.overview ? (
            <p className="max-w-3xl text-sm leading-7 text-muted-foreground sm:text-base">
              {item.overview}
            </p>
          ) : (
            <p className="text-sm text-muted-foreground">{t("mediaLibrary.noOverview")}</p>
          )}

          <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            {item.premiere_date ? (
              <span className="inline-flex items-center gap-1.5">
                <CalendarDays className="size-3.5" />
                {formatDate(item.premiere_date, locale)}
              </span>
            ) : null}
            {item.container ? <span>{item.container.toUpperCase()}</span> : null}
            {item.kind === "episode" && item.season_number && item.episode_number ? (
              <span>{episodeCode(item)}</span>
            ) : null}
          </div>
        </div>
      </div>

      {(isSeries || isSeason) ? (
        <div className="relative z-10 flex min-w-0 flex-col gap-5">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-xl font-semibold text-foreground">
              {isSeries ? t("mediaLibrary.seasons") : item.name}
            </h2>
            {isSeries && seasons.length > 0 ? (
              <Select
                value={selectedSeasonId === null ? "" : String(selectedSeasonId)}
                onValueChange={(value) => setSelectedSeasonId(Number(value))}
              >
                <SelectTrigger
                  aria-label={selectedSeason?.name ?? t("mediaLibrary.selectSeason")}
                  className="h-10 w-full border-border bg-muted/50 text-foreground sm:w-56"
                >
                  <SelectValue placeholder={t("mediaLibrary.selectSeason")} />
                </SelectTrigger>
                <SelectContent className="border-border bg-muted text-foreground">
                  <SelectGroup>
                    {seasons.map((season) => (
                      <SelectItem key={season.id} value={String(season.id)}>
                        {season.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            ) : null}
          </div>
          {selectedSeason?.overview ? (
            <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{selectedSeason.overview}</p>
          ) : null}
          {episodesQuery.isLoading ? (
            <div className="flex gap-4 overflow-hidden">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="w-[18rem] shrink-0 space-y-3">
                  <div className="aspect-video animate-pulse rounded-md bg-muted" />
                  <div className="h-4 w-3/4 animate-pulse rounded bg-muted" />
                  <div className="h-3 w-1/2 animate-pulse rounded bg-muted" />
                </div>
              ))}
            </div>
          ) : episodes.length > 0 ? (
			<>
			  <EpisodeRail
			    episodes={episodes}
			    highlightedEpisodeId={highlightedEpisodeId}
			    selectedEpisodeId={selectedEpisodeId}
			    onSelect={(episode) => {
			      setSelectedEpisodeId(episode.id)
			      setSelectedPerson(null)
			    }}
			    onPlay={onPlay}
			  />
			  {selectedEpisode?.user_data.played && onSetPlayed ? (
				<div className="mt-4 flex justify-end">
				  <Button
					type="button"
					variant="outline"
					className="h-11 border-border bg-transparent text-foreground hover:bg-muted"
					onClick={() => onSetPlayed(selectedEpisode, false)}
				  >
					<Circle data-icon="inline-start" />
					{t("mediaLibrary.markUnplayed")}
				  </Button>
				</div>
			  ) : null}
			</>
          ) : (
            <p className="text-sm text-muted-foreground">{t("mediaLibrary.noEpisodes")}</p>
          )}
        </div>
      ) : null}

	  {isAudio ? <MusicLyrics item={item} /> : null}

	  {peopleTarget ? (
		selectedPerson ? (
		  <PersonWorksPanel
			person={selectedPerson}
			items={personItems}
			isLoading={personItemsQuery.isLoading}
			isError={personItemsQuery.isError}
			hasMore={personItemsQuery.hasNextPage}
			isLoadingMore={personItemsQuery.isFetchingNextPage}
			onLoadMore={() => void personItemsQuery.fetchNextPage()}
			onBack={() => setSelectedPerson(null)}
			onRetry={() => void personItemsQuery.refetch()}
			onOpenItem={onOpenItem}
		  />
		) : (
		  <MediaInfoPanel
			item={peopleTarget}
			canManage={canManage}
			people={peopleQuery.data?.items ?? []}
			peopleLoading={peopleQuery.isLoading}
			peopleError={peopleQuery.isError}
			onRetryPeople={() => void peopleQuery.refetch()}
			onSelectPerson={setSelectedPerson}
			onEditMetadata={onEditMetadata}
		  />
		)
	  ) : null}
    </section>
  )
}

function MusicMetadata({ item }: { item: MediaLibraryItem }) {
	const { t } = useI18n()
	const artists = item.artists?.filter(Boolean) ?? []
	const artistText = artists.join(", ") || item.album_artist

	return (
		<dl className="grid gap-3 text-sm text-muted-foreground sm:grid-cols-2">
			{artistText ? (
				<MusicMetadataRow icon={<UsersRound />} label={t("mediaLibrary.artists")} value={artistText} />
			) : null}
			{item.album ? (
				<MusicMetadataRow icon={<Music2 />} label={t("mediaLibrary.album")} value={item.album} />
			) : null}
		</dl>
	)
}

function MusicMetadataRow({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
	return (
		<div className="min-w-0 rounded-lg border border-border bg-muted/50 px-3 py-2.5">
			<dt className="flex items-center gap-2 text-xs text-muted-foreground">{icon}{label}</dt>
			<dd className="mt-1 truncate text-sm text-foreground/80" title={value}>{value}</dd>
		</div>
	)
}

function MusicLyrics({ item }: { item: MediaLibraryItem }) {
	const { t } = useI18n()
	const original = displayLyrics(item.lyrics)
	const translated = displayLyrics(item.translated_lyrics)
	if (!original && !translated) return null

	return (
		<section data-music-lyrics className="relative z-10 border-t border-border pt-7">
			<h2 className="text-xl font-semibold text-foreground">{t("mediaLibrary.lyricsSection")}</h2>
			<div className="mt-4 grid gap-4 lg:grid-cols-2">
				{original ? <LyricsBlock title={t("mediaLibrary.lyrics")} value={original} /> : null}
				{translated ? <LyricsBlock title={t("mediaLibrary.translatedLyrics")} value={translated} /> : null}
			</div>
		</section>
	)
}

function LyricsBlock({ title, value }: { title: string; value: string }) {
	return (
		<div className="min-w-0 rounded-lg border border-border bg-muted/50 p-4">
			<h3 className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">{title}</h3>
			<p className="mt-3 max-h-80 overflow-y-auto whitespace-pre-wrap text-sm leading-7 text-muted-foreground">{value}</p>
		</div>
	)
}

function EpisodeRail({
  episodes,
  highlightedEpisodeId,
  selectedEpisodeId,
  onSelect,
  onPlay,
}: {
  episodes: MediaLibraryItem[]
  highlightedEpisodeId?: number
  selectedEpisodeId: number | null
  onSelect: (item: MediaLibraryItem) => void
  onPlay: (item: MediaLibraryItem) => void
}) {
  const { locale, t } = useI18n()
  const targetEpisodeRef = useRef<HTMLButtonElement>(null)
  const episodeShelfRef = useRef<HTMLDivElement>(null)
  const [scrollState, setScrollState] = useState({
    canScrollBack: false,
    canScrollForward: false,
  })

  const updateScrollState = useCallback(() => {
    const shelf = episodeShelfRef.current
    if (!shelf) return
    const nextState = {
      canScrollBack: shelf.scrollLeft > 1,
      canScrollForward: shelf.scrollLeft + shelf.clientWidth < shelf.scrollWidth - 1,
    }
    setScrollState((current) =>
      current.canScrollBack === nextState.canScrollBack &&
      current.canScrollForward === nextState.canScrollForward
        ? current
        : nextState,
    )
  }, [])

  useEffect(() => {
    const shelf = episodeShelfRef.current
    if (!shelf) return

    updateScrollState()
    window.addEventListener("resize", updateScrollState)
    const resizeObserver = new ResizeObserver(updateScrollState)
    resizeObserver.observe(shelf)
    return () => {
      window.removeEventListener("resize", updateScrollState)
      resizeObserver.disconnect()
    }
  }, [episodes.length, updateScrollState])

  useEffect(() => {
    targetEpisodeRef.current?.scrollIntoView?.({
      block: "nearest",
      inline: "center",
    })
  }, [episodes, highlightedEpisodeId])

  function scrollEpisodes(direction: -1 | 1) {
    const shelf = episodeShelfRef.current
    if (!shelf) return
    shelf.scrollBy({
      left: direction * Math.max(shelf.clientWidth * 0.8, 320),
      behavior: "smooth",
    })
  }

  return (
    <div className="relative">
      {scrollState.canScrollBack ? (
        <div className="pointer-events-none absolute top-0 bottom-0 left-0 z-10 w-24 bg-gradient-to-r from-background via-background/60 to-transparent" />
      ) : null}

      {scrollState.canScrollBack ? (
        <button
          type="button"
          aria-label={t("mediaLibrary.previousItems", { section: t("mediaLibrary.kind.episode") })}
          className="absolute top-1/2 left-2 z-20 hidden size-11 -translate-y-1/2 items-center justify-center rounded-full bg-background/95 text-foreground shadow-lg ring-1 ring-border backdrop-blur-sm transition hover:bg-background hover:ring-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 sm:flex"
          onClick={() => scrollEpisodes(-1)}
        >
          <ChevronLeft className="size-5" />
        </button>
      ) : null}

      <div
        ref={episodeShelfRef}
        className="flex snap-x snap-mandatory gap-4 overflow-x-auto pb-2 scrollbar-none"
        onScroll={updateScrollState}
      >
      {episodes.map((episode) => (
        <article
          key={episode.id}
          className={cn(
            "group relative w-[18rem] shrink-0 snap-start text-left transition-transform duration-200 hover:-translate-y-0.5 sm:w-[21rem]",
            selectedEpisodeId === episode.id && "rounded-md ring-2 ring-ring/70 ring-offset-4 ring-offset-background",
          )}
        >
          <div
            data-media-orientation="landscape"
            className="relative aspect-video overflow-hidden rounded-md bg-muted ring-1 ring-border transition group-hover:ring-primary/60"
          >
            {episode.image_url ? (
              <img
                src={episode.image_url}
                alt=""
                loading="lazy"
                className="size-full object-cover transition duration-200 group-hover:scale-[1.02]"
              />
            ) : (
              <div className="flex size-full items-center justify-center text-muted-foreground">
                <Tv className="size-10" strokeWidth={1.2} />
              </div>
            )}
            <span
              aria-hidden="true"
              className="absolute inset-0 z-10 cursor-pointer"
              onClick={() => onSelect(episode)}
            />
            <button
              type="button"
              aria-label={t("mediaLibrary.playEpisode", { name: episode.name })}
              className="absolute top-1/2 left-1/2 z-20 flex size-11 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg transition hover:scale-105 hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              onClick={() => onPlay(episode)}
            >
              <Play className="size-5 fill-current" />
            </button>
          </div>
          <button
            ref={highlightedEpisodeId === episode.id ? targetEpisodeRef : undefined}
            type="button"
            aria-label={`${episodeCode(episode)} ${episode.name}`}
            aria-current={highlightedEpisodeId === episode.id ? "true" : undefined}
            aria-pressed={selectedEpisodeId === episode.id}
            className="mt-3 w-full space-y-1 rounded-md text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
            onClick={() => onSelect(episode)}
          >
            <div className="flex items-center gap-2">
              <span className="text-xs font-medium tabular-nums text-primary">
                {episodeCode(episode)}
              </span>
              <span className="truncate text-sm font-medium text-foreground" title={episode.name}>
                {episode.name}
              </span>
            </div>
            <div className="flex items-center gap-3 text-xs text-muted-foreground">
              {episode.premiere_date ? <span>{formatDate(episode.premiere_date, locale)}</span> : null}
              {episode.runtime_ticks ? <span>{formatRuntime(episode.runtime_ticks)}</span> : null}
              {episode.community_rating ? (
                <span className="inline-flex items-center gap-1 text-primary">
                  <Star className="size-3 fill-current" />
                  {episode.community_rating.toFixed(1)}
                </span>
              ) : null}
            </div>
            {episode.overview ? (
              <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">{episode.overview}</p>
            ) : (
              <p className="text-xs text-muted-foreground">{t("mediaLibrary.noOverview")}</p>
            )}
          </button>
        </article>
      ))}
      </div>

      {scrollState.canScrollForward ? (
        <div className="pointer-events-none absolute top-0 bottom-0 right-0 z-10 w-24 bg-gradient-to-l from-background via-background/60 to-transparent" />
      ) : null}

      {scrollState.canScrollForward ? (
        <button
          type="button"
          aria-label={t("mediaLibrary.nextItems", { section: t("mediaLibrary.kind.episode") })}
          className="absolute top-1/2 right-2 z-20 hidden size-11 -translate-y-1/2 items-center justify-center rounded-full bg-background/95 text-foreground shadow-lg ring-1 ring-border backdrop-blur-sm transition hover:bg-background hover:ring-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 sm:flex"
          onClick={() => scrollEpisodes(1)}
        >
          <ChevronRight className="size-5" />
        </button>
      ) : null}
    </div>
  )
}

function MediaInfoPanel({
  item,
  canManage,
  people,
  peopleLoading,
  peopleError,
  onRetryPeople,
  onSelectPerson,
	onEditMetadata,
}: {
  item: MediaLibraryItem
  canManage: boolean
  people: MediaLibraryPerson[]
  peopleLoading: boolean
  peopleError: boolean
  onRetryPeople: () => void
  onSelectPerson: (person: MediaLibraryPerson) => void
	onEditMetadata?: (item: MediaLibraryItem) => void
}) {
  const { locale, t } = useI18n()
  const openListPath = item.media_path || item.path

  return (
    <section data-media-item-info className="relative z-10 border-t border-border pt-7">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">
		  {t(item.kind === "episode" ? "mediaLibrary.episodeInformation" : "mediaLibrary.mediaInformation")}
	    </h3>
        {canManage && onEditMetadata ? (
          <Button type="button" variant="ghost" size="icon-sm" aria-label={t("mediaLibrary.editMetadata")} onClick={() => onEditMetadata(item)}>
            <Edit3 />
          </Button>
        ) : null}
      </div>
      <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-2 xl:grid-cols-3">
        {canManage && openListPath ? (
          <MediaInfoRow icon={<FolderOpen />} label={t("mediaLibrary.openListLocation")} value={openListPath} mono />
        ) : null}
        <MediaInfoRow icon={<HardDrive />} label={t("mediaLibrary.fileSize")} value={formatBytes(item.size_bytes)} />
        <MediaInfoRow icon={<CalendarDays />} label={t("mediaLibrary.addedAt")} value={formatTimestamp(item.created_at, locale)} />
      </dl>

      <div className="mt-8">
        <h3 className="text-lg font-semibold text-foreground">{t("mediaLibrary.castAndCrew")}</h3>
        {peopleLoading ? (
          <div className="mt-4 flex gap-3 overflow-hidden">
            {Array.from({ length: 6 }).map((_, index) => (
              <div key={index} className="w-24 shrink-0 space-y-2">
                <div className="aspect-[3/4] animate-pulse rounded-md bg-muted" />
                <div className="h-3 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : peopleError ? (
          <div className="mt-4 flex items-center gap-3 text-sm text-muted-foreground">
            <span>{t("mediaLibrary.peopleLoadFailed")}</span>
            <Button type="button" variant="outline" size="sm" onClick={onRetryPeople}>
              {t("state.retry")}
            </Button>
          </div>
        ) : people.length > 0 ? (
          <div className="mt-4 flex gap-4 overflow-x-auto pb-2 scrollbar-none">
            {people.map((person) => (
              <button
                key={person.id}
                type="button"
                className="group w-24 shrink-0 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
                onClick={() => onSelectPerson(person)}
              >
                <div className="aspect-[3/4] overflow-hidden rounded-md bg-muted/50 ring-1 ring-border transition group-hover:ring-primary/60">
                  {person.profile_url ? (
                    <img src={person.profile_url} alt="" loading="lazy" className="size-full object-cover" />
                  ) : (
                    <div className="flex size-full items-center justify-center text-muted-foreground">
                      <UserRound className="size-8" strokeWidth={1.2} />
                    </div>
                  )}
                </div>
                <p className="mt-2 truncate text-sm font-medium text-foreground" title={person.name}>{person.name}</p>
				<p className="truncate text-xs text-muted-foreground" title={person.display_role || person.role || person.type}>{person.display_role || person.role || person.type}</p>
              </button>
            ))}
          </div>
        ) : (
          <p className="mt-4 text-sm text-muted-foreground">{t("mediaLibrary.noPeople")}</p>
        )}
      </div>
    </section>
  )
}

function MediaInfoRow({
  icon,
  label,
  value,
  mono = false,
}: {
  icon: ReactNode
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="min-w-0 border-l border-border pl-3">
      <dt className="flex items-center gap-2 text-xs text-muted-foreground">
        {icon}
        {label}
      </dt>
      <dd className={cn("mt-1 break-all text-sm text-foreground/80", mono && "font-mono text-xs")}>{value}</dd>
    </div>
  )
}

function PersonWorksPanel({
  person,
  items,
  isLoading,
  isError,
  onBack,
  onRetry,
  onOpenItem,
  hasMore,
  isLoadingMore,
  onLoadMore,
}: {
  person: MediaLibraryPerson
  hasMore: boolean
  isLoadingMore: boolean
  onLoadMore: () => void
  items: MediaLibraryItem[]
  isLoading: boolean
  isError: boolean
  onBack: () => void
  onRetry: () => void
  onOpenItem: (item: MediaLibraryItem) => void
}) {
  const { t } = useI18n()

  return (
    <section data-media-person-works className="border-t border-border pt-7">
      <div className="flex items-center gap-3">
        <Button type="button" variant="ghost" size="sm" className="-ml-3 min-h-11 shrink-0 text-muted-foreground hover:bg-muted hover:text-accent-foreground" onClick={onBack}>
          <ArrowLeft data-icon="inline-start" />
          {t("mediaLibrary.backToEpisode")}
        </Button>
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted/50">
            {person.profile_url ? <img src={person.profile_url} alt="" className="size-full object-cover" /> : <UserRound className="size-5 text-muted-foreground" />}
          </div>
          <h3 className="truncate text-lg font-semibold text-foreground">{t("mediaLibrary.personWorks", { name: person.name })}</h3>
        </div>
      </div>
      {isLoading ? (
        <div className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          {Array.from({ length: 5 }).map((_, index) => <div key={index} className="aspect-[2/3] animate-pulse rounded-md bg-muted" />)}
        </div>
      ) : isError && items.length === 0 ? (
        <div className="mt-5 flex items-center gap-3 text-sm text-muted-foreground">
          <span>{t("mediaLibrary.personWorksLoadFailed")}</span>
          <Button type="button" variant="outline" size="sm" className="min-h-11" onClick={onRetry}>{t("state.retry")}</Button>
        </div>
      ) : items.length > 0 ? (
        <div className="mt-5">
          <MediaItemCollection items={items} onOpen={onOpenItem} showEpisodeSeries showGroupHeadings={false} />
          {isError ? <div className="mt-5 flex items-center gap-3 text-sm text-muted-foreground">
            <span>{t("mediaLibrary.personWorksLoadFailed")}</span>
            <Button type="button" variant="outline" className="min-h-11" onClick={hasMore ? onLoadMore : onRetry}>{t("state.retry")}</Button>
          </div> : hasMore ? <Button type="button" variant="outline" className="mt-5 min-h-11" disabled={isLoadingMore} onClick={onLoadMore}>{t("mediaLibrary.loadMore")}</Button> : null}
        </div>
      ) : (
        <p className="mt-5 text-sm text-muted-foreground">{t("mediaLibrary.noPersonWorks")}</p>
      )}
    </section>
  )
}

function episodeCode(item: MediaLibraryItem) {
  return `S${String(item.season_number ?? 0).padStart(2, "0")}E${String(item.episode_number ?? 0).padStart(2, "0")}`
}

function formatRuntime(ticks: number) {
  const minutes = Math.max(1, Math.round(ticks / 10_000_000 / 60))
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60)
    const remaining = minutes % 60
    return remaining ? `${hours}h ${remaining}m` : `${hours}h`
  }
  return `${minutes}m`
}

function formatDate(value: string, locale: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric" }).format(date)
}

function playbackProgress(item: MediaLibraryItem) {
  const runtime = item.runtime_ticks ?? 0
  if (runtime <= 0) return "32%"
  return `${Math.min(100, (item.user_data.playback_position_ticks / runtime) * 100)}%`
}
