import { KeyboardEvent, useEffect, useState } from "react"
import {
  ChevronDown,
  ChevronRight,
  Film,
  History,
  Search,
  Star,
  Tv,
} from "lucide-react"
import { useMutation } from "@tanstack/react-query"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  getMediaDetail,
  ManagerApiError,
  MediaDetail,
  MetadataSource,
  MetadataSourceMode,
  MediaEpisode,
  MediaSearchResult,
  MediaSeason,
  searchMedia,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { cn } from "@/lib/utils"
import {
  buildEpisodeRename,
  buildMovieRename,
  formatEpisodeCode,
  formatSeasonCode,
  getMediaDisplayTitle,
} from "./mediaNaming"

interface MediaSearchPanelProps {
  originalName: string
  isDirectory: boolean
  onSelectName: (name: string) => void
}

export const MEDIA_SEARCH_HISTORY_STORAGE_KEY = "openlist2emby.media-search-history"
export const MEDIA_SEARCH_HISTORY_LIMIT = 10

export interface MediaEpisodePickerSelection {
  item: MediaSearchResult
  season: MediaSeason
  episode: MediaEpisode
  episodes: MediaEpisode[]
}

interface MediaPickerProps {
  source?: MetadataSourceMode
  onMovieSelect?: (item: MediaSearchResult) => void
  onTVSelect?: (item: MediaSearchResult) => void
  onEpisodeSelect: (selection: MediaEpisodePickerSelection | null) => void
  movieOnly?: boolean
  tvOnly?: boolean
  showSearchHistory?: boolean
}

type DetailKey = string

function getResultKey(item: MediaSearchResult) {
  return `${item.source ?? "tmdb"}:${item.media_type}:${item.id}`
}

function getPosterURL(path?: string, url?: string) {
  if (url) {
    return url
  }
  if (!path) {
    return ""
  }
  if (path.startsWith("http://") || path.startsWith("https://")) {
    return path
  }
  return `https://image.tmdb.org/t/p/w500${path.startsWith("/") ? path : `/${path}`}`
}

function getSeasonDetailKey(result: MediaSearchResult, seasonNumber: number) {
  return `${getResultKey(result)}:season:${seasonNumber}`
}

function getErrorMessage(error: unknown, fallback: string) {
  return error instanceof ManagerApiError ? error.message : fallback
}

function readSearchHistory() {
  try {
    const stored = window.localStorage.getItem(MEDIA_SEARCH_HISTORY_STORAGE_KEY)
    if (!stored) {
      return []
    }
    const parsed: unknown = JSON.parse(stored)
    if (!Array.isArray(parsed)) {
      return []
    }
    const unique = new Set<string>()
    for (const value of parsed) {
      if (typeof value !== "string") {
        continue
      }
      const query = value.trim()
      if (query) {
        unique.add(query)
      }
      if (unique.size >= MEDIA_SEARCH_HISTORY_LIMIT) {
        break
      }
    }
    return [...unique]
  } catch {
    return []
  }
}

function writeSearchHistory(history: string[]) {
  try {
    window.localStorage.setItem(
      MEDIA_SEARCH_HISTORY_STORAGE_KEY,
      JSON.stringify(history),
    )
  } catch {
    // Search history is an enhancement; storage failures must not block search.
  }
}

function Poster({
  path,
  url,
  alt,
  className,
  iconSize = "size-5",
}: {
  path?: string
  url?: string
  alt: string
  className: string
  iconSize?: string
}) {
  const [failed, setFailed] = useState(false)
  const src = getPosterURL(path, url)

  useEffect(() => {
    setFailed(false)
  }, [src])

  if (!src || failed) {
    return (
      <div
        className={cn(
          "flex shrink-0 items-center justify-center bg-muted text-muted-foreground",
          className,
        )}
        aria-label={alt}
      >
        <Film className={iconSize} strokeWidth={1.7} />
      </div>
    )
  }

  return (
    <img
      src={src}
      alt={alt}
      className={cn("shrink-0 bg-muted object-cover", className)}
      loading="lazy"
      onError={() => setFailed(true)}
    />
  )
}

export function MediaSearchPanel({
  originalName,
  isDirectory,
  onSelectName,
}: MediaSearchPanelProps) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    setExpanded(false)
  }, [isDirectory, originalName])

  return (
    <div className="flex flex-col gap-2">
      <Button
        type="button"
        variant="outline"
        className="w-full justify-between"
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
      >
        <span className="flex items-center gap-2">
          <Search className="size-4" />
          {t("media.toggleSearch")}
        </span>
        {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
      </Button>

      {expanded ? (
        <MediaPicker
          key={`${isDirectory ? "directory" : "file"}:${originalName}`}
          onMovieSelect={(item) =>
            onSelectName(buildMovieRename(item, originalName, isDirectory))
          }
          onEpisodeSelect={(selection) => {
            if (selection) {
              onSelectName(
                buildEpisodeRename(
                  selection.item,
                  selection.season,
                  selection.episode,
                  originalName,
                  isDirectory,
                ),
              )
            }
          }}
        />
      ) : null}
    </div>
  )
}

export function MediaPicker({
  source,
  onMovieSelect,
  onTVSelect,
  onEpisodeSelect,
  movieOnly = false,
  tvOnly = false,
  showSearchHistory = false,
}: MediaPickerProps) {
  const { locale, t } = useI18n()
  const [keyword, setKeyword] = useState("")
  const [searchHistory, setSearchHistory] = useState<string[]>(readSearchHistory)
  const [results, setResults] = useState<MediaSearchResult[]>([])
  const [searched, setSearched] = useState(false)
  const [selectedKey, setSelectedKey] = useState<DetailKey | null>(null)
  const [details, setDetails] = useState<Record<DetailKey, MediaDetail>>({})
  const [detailErrors, setDetailErrors] = useState<Record<DetailKey, string>>({})
  const [loadingDetailKey, setLoadingDetailKey] = useState<DetailKey | null>(null)
  const [selectedSeasonKey, setSelectedSeasonKey] = useState<DetailKey | null>(null)
  const [selectedEpisodeKey, setSelectedEpisodeKey] = useState<string | null>(null)

  const searchMutation = useMutation({
    mutationFn: ({ query, language }: { query: string; language: string }) =>
      searchMedia(query, language, source),
  })
  const detailMutation = useMutation({
    mutationFn: ({
      type,
      id,
      language,
      season,
      provider,
    }: {
      type: "movie" | "tv"
      id: number
      language: string
      season?: number
      provider?: MetadataSource
    }) => getMediaDetail(type, id, language, season, provider),
  })

  const language = locale.startsWith("zh") ? "zh-CN" : "en-US"

  function recordSearch(query: string) {
    setSearchHistory((previous) => {
      const next = [query, ...previous.filter((entry) => entry !== query)].slice(
        0,
        MEDIA_SEARCH_HISTORY_LIMIT,
      )
      writeSearchHistory(next)
      return next
    })
  }

  async function handleSearch(nextKeyword = keyword) {
    const query = nextKeyword.trim()
    if (!query) {
      return
    }
    if (nextKeyword !== keyword) {
      setKeyword(query)
    }
    recordSearch(query)
    setSearched(false)
    setResults([])
    setSelectedKey(null)
    setDetails({})
    setDetailErrors({})
    setSelectedSeasonKey(null)
    setSelectedEpisodeKey(null)
    onEpisodeSelect(null)
    try {
      const response = await searchMutation.mutateAsync({ query, language })
      setResults(
        tvOnly
          ? (response.results ?? []).filter((item) => item.media_type === "tv")
          : movieOnly
            ? (response.results ?? []).filter((item) => item.media_type === "movie")
            : (response.results ?? []),
      )
      setSearched(true)
    } catch {
      setSearched(true)
    }
  }

  async function loadDetail(item: MediaSearchResult, season?: number) {
    const key = season === undefined ? getResultKey(item) : getSeasonDetailKey(item, season)
    setLoadingDetailKey(key)
    setDetailErrors((previous) => ({ ...previous, [key]: "" }))
    try {
      const detail = await detailMutation.mutateAsync({
        provider: item.source,
        type: item.media_type,
        id: item.id,
        language,
        season,
      })
      setDetails((previous) => ({ ...previous, [key]: detail }))
    } catch (error) {
      setDetailErrors((previous) => ({
        ...previous,
        [key]: getErrorMessage(error, t("media.detailFailed")),
      }))
    } finally {
      setLoadingDetailKey((current) => (current === key ? null : current))
    }
  }

  function handleResultClick(item: MediaSearchResult) {
    const key = getResultKey(item)
    setSelectedEpisodeKey(null)
    onEpisodeSelect(null)
    if (item.media_type === "movie") {
      setSelectedKey(key)
      onMovieSelect?.(item)
      return
    }
    if (selectedKey === key) {
      setSelectedKey(null)
      setSelectedSeasonKey(null)
      return
    }
    setSelectedKey(key)
    setSelectedSeasonKey(null)
    if (!details[key]) {
      void loadDetail(item)
    }
  }

  async function handleSeasonClick(item: MediaSearchResult, season: MediaSeason) {
    const seasonKey = getSeasonDetailKey(item, season.season_number)
    setSelectedEpisodeKey(null)
    onEpisodeSelect(null)
    if (selectedSeasonKey === seasonKey) {
      setSelectedSeasonKey(null)
      return
    }
    setSelectedSeasonKey(seasonKey)
    if (!details[seasonKey]) {
      await loadDetail(item, season.season_number)
    }
  }

  function handleEpisodeClick(
    item: MediaSearchResult,
    season: MediaSeason,
    episode: MediaEpisode,
  ) {
    const key = `${getSeasonDetailKey(item, season.season_number)}:${episode.episode_number}`
    setSelectedEpisodeKey(key)
    onEpisodeSelect({
      item,
      season,
      episode,
      episodes: details[getSeasonDetailKey(item, season.season_number)]?.episodes ?? [],
    })
  }

  function handleResultKeyDown(
    event: KeyboardEvent<HTMLDivElement>,
    item: MediaSearchResult,
  ) {
    if (event.target !== event.currentTarget) {
      return
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault()
      handleResultClick(item)
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-muted/20 p-3">
      <div className="flex gap-2" role="search">
        <Input
          aria-label={t("media.searchPlaceholder")}
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault()
              void handleSearch()
            }
          }}
          placeholder={t("media.searchPlaceholder")}
          className="min-w-0 flex-1"
        />
        <Button
          type="button"
          size="icon-lg"
          aria-label={t("media.search")}
          disabled={searchMutation.isPending || !keyword.trim()}
          onClick={() => void handleSearch()}
        >
          {searchMutation.isPending ? <Spinner /> : <Search />}
        </Button>
      </div>

      {showSearchHistory && searchHistory.length > 0 ? (
        <div className="flex flex-col gap-2 border-t pt-3">
          <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <History className="size-3.5" />
            {t("media.searchHistory")}
          </div>
          <div className="flex flex-wrap gap-2">
            {searchHistory.map((query) => (
              <Button
                key={query}
                type="button"
                variant="outline"
                size="sm"
                className="max-w-full"
                title={query}
                aria-label={t("media.searchHistorySelect", { query })}
                onClick={() => void handleSearch(query)}
              >
                <span className="max-w-52 truncate">{query}</span>
              </Button>
            ))}
          </div>
        </div>
      ) : null}

      {searchMutation.isError ? (
        <p role="alert" className="break-words text-sm text-destructive">
          {getErrorMessage(searchMutation.error, t("media.noResults"))}
        </p>
      ) : null}

      {searchMutation.isPending ? <SearchSkeleton /> : null}
      {!searchMutation.isPending && searched && results.length === 0 && !searchMutation.isError ? (
        <p className="py-2 text-center text-sm text-muted-foreground">
          {t("media.noResults")}
        </p>
      ) : null}

      {!searchMutation.isPending && results.length > 0 ? (
        <div className="flex flex-col gap-2">
          {results.map((item) => {
            const key = getResultKey(item)
            const detail = details[key]
            const isSelected = selectedKey === key
            return (
              <MediaResultCard
                key={key}
                item={item}
                detail={detail}
                isSelected={isSelected}
                loadingDetail={loadingDetailKey === key}
                detailError={detailErrors[key]}
                detailErrors={detailErrors}
                selectedSeasonKey={selectedSeasonKey}
                selectedEpisodeKey={selectedEpisodeKey}
                details={details}
                loadingSeasonKey={loadingDetailKey}
                onClick={() => handleResultClick(item)}
                onKeyDown={(event) => handleResultKeyDown(event, item)}
                onSelect={item.media_type === "tv" ? () => onTVSelect?.(item) : undefined}
                onSeasonClick={(season) => void handleSeasonClick(item, season)}
                onEpisodeClick={(season, episode) =>
                  handleEpisodeClick(item, season, episode)
                }
                t={t}
              />
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

function SearchSkeleton() {
  return (
    <div className="flex gap-3 rounded-lg border bg-card p-3">
      <Skeleton className="h-24 w-16 shrink-0 rounded-md" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-3 w-1/2" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-4/5" />
      </div>
    </div>
  )
}

function MediaResultCard({
  item,
  detail,
  isSelected,
  loadingDetail,
  detailError,
  detailErrors,
  selectedSeasonKey,
  selectedEpisodeKey,
  details,
  loadingSeasonKey,
  onClick,
  onKeyDown,
  onSelect,
  onSeasonClick,
  onEpisodeClick,
  t,
}: {
  item: MediaSearchResult
  detail?: MediaDetail
  isSelected: boolean
  loadingDetail: boolean
  detailError?: string
  detailErrors: Record<DetailKey, string>
  selectedSeasonKey: string | null
  selectedEpisodeKey: string | null
  details: Record<DetailKey, MediaDetail>
  loadingSeasonKey: string | null
  onClick: () => void
  onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void
  onSelect?: () => void
  onSeasonClick: (season: MediaSeason) => void
  onEpisodeClick: (season: MediaSeason, episode: MediaEpisode) => void
  t: ReturnType<typeof useI18n>["t"]
}) {
  const title = getMediaDisplayTitle(item)
  const date = item.first_air_date || item.release_date
  const isTV = item.media_type === "tv"

  return (
    <div
      className={cn(
        "rounded-lg border bg-card p-3 transition-colors",
        isSelected && "border-primary/60 bg-primary/5",
      )}
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={isTV ? isSelected : undefined}
        aria-label={t("media.selectResult", { title })}
        className="flex cursor-pointer gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        onClick={onClick}
        onKeyDown={onKeyDown}
      >
        <Poster
          path={item.poster_path}
          url={item.poster_url}
          alt={title}
          className="h-24 w-16 rounded-md"
        />
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex min-w-0 items-start justify-between gap-2">
            <span className="line-clamp-2 min-w-0 text-sm font-semibold leading-snug">
              {title}
            </span>
            <Badge variant="outline" className="shrink-0">
              {isTV ? <Tv className="size-3" /> : <Film className="size-3" />}
              {isTV ? t("media.tv") : t("media.movie")}
            </Badge>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            {item.source ? <Badge variant="secondary">{item.source.toUpperCase()}</Badge> : null}
            {date ? <span>{date}</span> : null}
            {item.vote_average > 0 ? (
              <span className="inline-flex items-center gap-1">
                <Star className="size-3 fill-current" />
                {item.vote_average.toFixed(1)}
              </span>
            ) : null}
          </div>
          {item.overview ? (
            <p className="line-clamp-3 text-xs leading-relaxed text-muted-foreground">
              {item.overview}
            </p>
          ) : null}
        </div>
      </div>

      {isTV && onSelect ? (
        <div className="mt-3 flex justify-end border-t pt-3">
          <Button
            type="button"
            variant="outline"
            aria-label={t("media.selectTV")}
            onClick={onSelect}
          >
            {t("media.selectTV")}
          </Button>
        </div>
      ) : null}

      {isSelected && isTV ? (
        <div className="mt-3 border-t pt-3">
          {loadingDetail ? <p className="text-xs text-muted-foreground">{t("media.loadingDetails")}</p> : null}
          {detailError ? <p className="text-xs text-destructive">{detailError}</p> : null}
          {!loadingDetail && !detailError && detail ? (
            <SeasonList
              item={item}
              detail={detail}
              details={details}
              detailErrors={detailErrors}
              loadingSeasonKey={loadingSeasonKey}
              selectedSeasonKey={selectedSeasonKey}
              selectedEpisodeKey={selectedEpisodeKey}
              onSeasonClick={onSeasonClick}
              onEpisodeClick={onEpisodeClick}
              t={t}
            />
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function SeasonList({
  item,
  detail,
  details,
  detailErrors,
  loadingSeasonKey,
  selectedSeasonKey,
  selectedEpisodeKey,
  onSeasonClick,
  onEpisodeClick,
  t,
}: {
  item: MediaSearchResult
  detail: MediaDetail
  details: Record<DetailKey, MediaDetail>
  detailErrors: Record<DetailKey, string>
  loadingSeasonKey: string | null
  selectedSeasonKey: string | null
  selectedEpisodeKey: string | null
  onSeasonClick: (season: MediaSeason) => void
  onEpisodeClick: (season: MediaSeason, episode: MediaEpisode) => void
  t: ReturnType<typeof useI18n>["t"]
}) {
  const seasons = detail.seasons ?? []
  if (seasons.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("media.noSeasons")}</p>
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between text-xs font-medium text-muted-foreground">
        <span>{t("media.seasons", { count: detail.number_of_seasons ?? seasons.length })}</span>
      </div>
      <div className="flex flex-col gap-2">
        {seasons.map((season) => {
          const seasonKey = getSeasonDetailKey(item, season.season_number)
          const seasonDetail = details[seasonKey]
          const isSelected = selectedSeasonKey === seasonKey
          const isLoading = isSelected && loadingSeasonKey === seasonKey
          const episodeKey = selectedEpisodeKey?.startsWith(`${seasonKey}:`)
            ? selectedEpisodeKey
            : null
          return (
            <div
              key={seasonKey}
              className={cn(
                "shrink-0 rounded-md border bg-muted/20 p-2",
                isSelected && "border-primary/50 bg-primary/5",
              )}
            >
              <button
                type="button"
                className="flex min-h-14 w-full items-start gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                aria-label={t("media.selectSeason", { season: season.name })}
                onClick={() => onSeasonClick(season)}
              >
                <Poster
                  path={season.poster_path}
                  url={season.poster_url}
                  alt={season.name}
                  className="h-14 w-10 rounded"
                  iconSize="size-4"
                />
                <span className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="flex items-start justify-between gap-2 text-xs font-medium">
                    <span className="truncate">{season.name}</span>
                    <span className="shrink-0 text-muted-foreground">
                      {formatSeasonCode(season.season_number)}
                    </span>
                  </span>
                  <span className="text-[11px] text-muted-foreground">
                    {t("media.episodes", { count: season.episode_count })}
                    {season.air_date ? ` · ${season.air_date}` : ""}
                  </span>
                  {season.overview ? (
                    <span className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">
                      {season.overview}
                    </span>
                  ) : null}
                </span>
              </button>

              {isLoading ? (
                <div className="mt-2 flex items-center gap-2 border-t pt-2 text-xs text-muted-foreground">
                  <Spinner />
                  {t("media.loadingDetails")}
                </div>
              ) : null}
              {isSelected && detailErrors[seasonKey] ? (
                <p className="mt-2 border-t pt-2 text-xs text-destructive">
                  {detailErrors[seasonKey]}
                </p>
              ) : null}
              {isSelected && !isLoading && !detailErrors[seasonKey] && seasonDetail ? (
                <EpisodeList
                  item={item}
                  season={season}
                  episodes={seasonDetail.episodes ?? []}
                  selectedEpisodeKey={episodeKey}
                  onEpisodeClick={onEpisodeClick}
                  t={t}
                />
              ) : null}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function EpisodeList({
  item,
  season,
  episodes,
  selectedEpisodeKey,
  onEpisodeClick,
  t,
}: {
  item: MediaSearchResult
  season: MediaSeason
  episodes: MediaEpisode[]
  selectedEpisodeKey: string | null
  onEpisodeClick: (season: MediaSeason, episode: MediaEpisode) => void
  t: ReturnType<typeof useI18n>["t"]
}) {
  if (episodes.length === 0) {
    return <p className="mt-2 border-t pt-2 text-xs text-muted-foreground">{t("media.noEpisodes")}</p>
  }

  return (
    <div className="mt-3 flex flex-col gap-2 border-t pt-3">
      {episodes.map((episode) => {
        const key = `${getSeasonDetailKey(item, season.season_number)}:${episode.episode_number}`
        const isSelected = key === selectedEpisodeKey
        return (
          <button
            type="button"
            key={key}
            className={cn(
              "grid min-h-28 shrink-0 grid-cols-[6rem_minmax(0,1fr)] items-start gap-3 overflow-hidden rounded-lg border border-transparent p-3 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              isSelected ? "bg-primary/10" : "hover:bg-muted",
            )}
            aria-label={t("media.selectEpisode", { episode: episode.episode_number })}
            onClick={() => onEpisodeClick(season, episode)}
          >
            <Poster
              path={episode.still_path}
              url={episode.still_url}
              alt={episode.name}
              className="h-16 w-24 rounded-md object-cover"
              iconSize="size-4"
            />
            <span className="flex min-w-0 flex-col gap-1.5 overflow-hidden">
              <span className="flex items-start justify-between gap-3 text-sm font-medium leading-snug">
                <span className="line-clamp-1 min-w-0">
                  {episode.name || `Episode ${episode.episode_number}`}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {formatEpisodeCode(episode.episode_number)}
                </span>
              </span>
              <span className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                {episode.air_date ? <span>{episode.air_date}</span> : null}
                {episode.runtime ? <span>{t("media.minutes", { count: episode.runtime })}</span> : null}
              </span>
              {episode.overview ? (
                <span className="line-clamp-2 overflow-hidden text-xs leading-relaxed text-muted-foreground">
                  {episode.overview}
                </span>
              ) : null}
            </span>
          </button>
        )
      })}
    </div>
  )
}
