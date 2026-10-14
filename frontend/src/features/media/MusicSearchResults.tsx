import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Album, ChevronLeft, ChevronRight, Music2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { getMusicDetail, MusicTrack } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

const pageSize = 6

interface MusicSearchResultsProps {
  disabled?: boolean
  selectedTrack: MusicTrack | null
  tracks: MusicTrack[]
  onSelect: (track: MusicTrack) => void
}

function getTrackKey(track: MusicTrack) {
  return `${track.source}:${track.id}`
}

export function MusicSearchResults({
  disabled,
  selectedTrack,
  tracks,
  onSelect,
}: MusicSearchResultsProps) {
  const { t } = useI18n()
  const [page, setPage] = useState(1)
  const totalPages = Math.ceil(tracks.length / pageSize)

  useEffect(() => {
    setPage(1)
  }, [tracks])

  if (tracks.length === 0) return null

  const start = (page - 1) * pageSize
  const visibleTracks = tracks.slice(start, start + pageSize)

  return (
    <div aria-label={t("music.results")} className="space-y-3">
      <div className="grid gap-2 sm:grid-cols-2">
        {visibleTracks.map((track) => (
          <MusicSearchResult
            disabled={disabled}
            key={getTrackKey(track)}
            selected={
              selectedTrack !== null &&
              getTrackKey(selectedTrack) === getTrackKey(track)
            }
            track={track}
            onSelect={onSelect}
          />
        ))}
      </div>
      {totalPages > 1 ? (
        <div className="flex items-center justify-center gap-2 border-t pt-3">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label={t("music.previousPage")}
            disabled={disabled || page <= 1}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            <ChevronLeft />
          </Button>
          <span className="min-w-16 text-center text-xs tabular-nums text-muted-foreground">
            {t("music.page", { page, total: totalPages })}
          </span>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label={t("music.nextPage")}
            disabled={disabled || page >= totalPages}
            onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
          >
            <ChevronRight />
          </Button>
        </div>
      ) : null}
    </div>
  )
}

function MusicSearchResult({
  disabled,
  selected,
  track,
  onSelect,
}: {
  disabled?: boolean
  selected: boolean
  track: MusicTrack
  onSelect: (track: MusicTrack) => void
}) {
  const { t } = useI18n()
  const [coverFailed, setCoverFailed] = useState(false)
  const coverQuery = useQuery({
    queryKey: ["music-cover", track.source, track.picture_id],
    queryFn: () => getMusicDetail(track.source, track.picture_id, ""),
    enabled: track.picture_id !== "",
    staleTime: 30 * 60 * 1000,
  })

  const coverURL = coverQuery.data?.cover_url

  return (
    <button
      type="button"
      aria-pressed={selected}
      disabled={disabled}
      className={cn(
        "flex min-h-20 w-full items-center gap-3 rounded-lg border bg-background p-2.5 text-left outline-none transition-colors hover:bg-muted/60 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-60",
        selected && "border-primary bg-primary/5 ring-1 ring-primary/20",
      )}
      onClick={() => onSelect(track)}
    >
      <div className="relative flex size-14 shrink-0 items-center justify-center overflow-hidden rounded-md bg-file-audio/10 text-file-audio ring-1 ring-file-audio/15">
        <Music2 className="size-5" />
        {coverURL && !coverFailed ? (
          <img
            alt=""
            className="absolute inset-0 size-full object-cover"
            loading="lazy"
            src={coverURL}
            onError={() => setCoverFailed(true)}
          />
        ) : null}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium" title={track.name}>
          {track.name}
        </div>
        <div className="mt-1 truncate text-xs text-muted-foreground">
          {track.artists.join(" / ") || t("music.unknownArtist")}
        </div>
        {track.album ? (
          <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
            <Album className="size-3.5 shrink-0" />
            <span className="truncate">{track.album}</span>
          </div>
        ) : null}
      </div>
    </button>
  )
}
