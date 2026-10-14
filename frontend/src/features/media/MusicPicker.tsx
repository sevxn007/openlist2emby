import { FormEvent, useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Search } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { MusicSearchResults } from "@/features/media/MusicSearchResults"
import {
  getMusicDetail,
  MusicDetail,
  MusicSource,
  MusicTrack,
  searchMusic,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

interface MusicPickerProps {
  initialQuery: string
  onSelect: (track: MusicTrack, detail: MusicDetail) => void
}

function getErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

export function MusicPicker({ initialQuery, onSelect }: MusicPickerProps) {
  const { t } = useI18n()
  const [queryInput, setQueryInput] = useState(initialQuery)
  const [submittedQuery, setSubmittedQuery] = useState("")
  const [selectedTrack, setSelectedTrack] = useState<MusicTrack | null>(null)
  const [source, setSource] = useState<MusicSource>("netease")

  useEffect(() => {
    setQueryInput(initialQuery)
    setSubmittedQuery("")
    setSelectedTrack(null)
  }, [initialQuery])

  const searchQuery = useQuery({
    queryKey: ["music-search", submittedQuery, source],
    queryFn: () => searchMusic(submittedQuery, source),
    enabled: submittedQuery !== "",
    staleTime: (query) => query.state.data?.results.length ? 5 * 60 * 1000 : 0,
  })

  const detailQuery = useQuery({
    queryKey: [
      "music-detail",
      selectedTrack?.source,
      selectedTrack?.picture_id,
      selectedTrack?.lyric_id,
    ],
    queryFn: () =>
      getMusicDetail(
        selectedTrack?.source ?? "netease",
        selectedTrack?.picture_id ?? "",
        selectedTrack?.lyric_id ?? "",
      ),
    enabled: selectedTrack !== null,
    staleTime: 5 * 60 * 1000,
  })

  const tracks = searchQuery.data?.results ?? []

  function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = queryInput.trim()
    if (!nextQuery) return
    setSelectedTrack(null)
    if (nextQuery === submittedQuery) {
      void searchQuery.refetch()
      return
    }
    setSubmittedQuery(nextQuery)
  }

  return (
    <div className="space-y-4">
      <form
        className="flex gap-2"
        onSubmit={handleSearch}
      >
        <Select
          value={source}
          onValueChange={(value) => {
            setSource(value as MusicSource)
            setSelectedTrack(null)
            setSubmittedQuery("")
          }}
        >
          <SelectTrigger className="w-32" aria-label={t("music.source")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="netease">{t("music.sourceNetease")}</SelectItem>
              <SelectItem value="qmusic">{t("music.sourceQQ")}</SelectItem>
              <SelectItem value="kuwo">{t("music.sourceKuwo")}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
        <Input
          className="min-w-0 flex-1"
          aria-label={t("music.searchQuery")}
          value={queryInput}
          onChange={(event) => setQueryInput(event.target.value)}
          placeholder={t("music.searchPlaceholder")}
        />
        <Button type="submit" disabled={!queryInput.trim() || searchQuery.isFetching}>
          {searchQuery.isFetching ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <Search data-icon="inline-start" />
          )}
          {t("music.search")}
        </Button>
      </form>

      {searchQuery.isError ? (
        <p className="text-sm text-destructive" role="alert">
          {getErrorMessage(searchQuery.error, t("music.searchFailed"))}
        </p>
      ) : null}

      {searchQuery.isSuccess && tracks.length === 0 ? (
        <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
          {t("music.noResults")}
        </div>
      ) : null}

      <MusicSearchResults
        selectedTrack={selectedTrack}
        tracks={tracks}
        onSelect={setSelectedTrack}
      />

      {selectedTrack ? (
        <div className="flex items-center justify-between gap-3 border-t pt-4">
          <div className="min-w-0 text-sm">
            <div className="truncate font-medium">{selectedTrack.name}</div>
            {detailQuery.isError ? (
              <div className="mt-1 text-destructive" role="alert">
                {getErrorMessage(detailQuery.error, t("music.detailFailed"))}
              </div>
            ) : (
              <div className="mt-1 truncate text-muted-foreground">
                {selectedTrack.artists.join(" / ") || t("music.unknownArtist")}
              </div>
            )}
          </div>
          <Button
            type="button"
            disabled={!detailQuery.data || detailQuery.isFetching}
            onClick={() => detailQuery.data && onSelect(selectedTrack, detailQuery.data)}
          >
            {detailQuery.isFetching ? <Spinner data-icon="inline-start" /> : null}
            {t("music.useSelection")}
          </Button>
        </div>
      ) : null}
    </div>
  )
}
