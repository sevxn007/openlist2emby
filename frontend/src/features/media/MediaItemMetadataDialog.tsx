import {FormEvent, useEffect, useRef, useState} from "react"
import {useMutation, useQueryClient} from "@tanstack/react-query"
import {Camera, ImageIcon, Save, Search, Upload} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,} from "@/components/ui/dialog"
import {Field, FieldError, FieldGroup, FieldLabel} from "@/components/ui/field"
import {Input} from "@/components/ui/input"
import {Spinner} from "@/components/ui/spinner"
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from "@/components/ui/select"
import {Textarea} from "@/components/ui/textarea"
import {MediaEpisodePickerSelection, MediaPicker} from "@/features/files/MediaSearchPanel"
import {MediaImagePicker} from "./MediaImagePicker"
import {MusicPicker} from "@/features/media/MusicPicker"
import {
  captureMediaLibraryItemPrimaryImage,
  ManagerApiError,
  MediaImageSelection,
  MediaLibraryItem,
  MediaLibraryMetadataInput,
  MetadataRefreshRequest,
  MetadataSourceMode,
  refreshMediaLibraryItemMetadata,
  selectMediaLibraryItemImage,
  updateMediaLibraryItemMetadata,
  uploadMediaLibraryItemImage,
} from "@/shared/api/manager"
import {useI18n} from "@/shared/i18n/useI18n"

const ticksPerMinute = 600_000_000

function useFilePreview(file: File | null) {
  const [url, setURL] = useState("")
  useEffect(() => {
    if (!file) {
      setURL("")
      return
    }
    const nextURL = URL.createObjectURL(file)
    setURL(nextURL)
    return () => URL.revokeObjectURL(nextURL)
  }, [file])
  return url
}

function ImageUploadField({
  id,
  label,
  previewURL,
  file,
  disabled,
  capturePending = false,
  onCapture,
                              onSearch,
  onChange,
}: {
  id: string
  label: string
  previewURL: string
  file: File | null
  disabled: boolean
  capturePending?: boolean
    onSearch?: () => void
  onCapture?: () => void
  onChange: (file: File) => void
}) {
  const { t } = useI18n()
  const inputRef = useRef<HTMLInputElement>(null)
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <input
        ref={inputRef}
        id={id}
        className="sr-only"
        type="file"
        accept="image/jpeg,image/png,image/webp,image/gif"
        disabled={disabled}
        onChange={(event) => {
          const selected = event.target.files?.[0]
          if (selected) onChange(selected)
          event.target.value = ""
        }}
      />
      <div className="flex min-w-0 flex-col items-stretch gap-3 rounded-md border p-3 sm:flex-row sm:items-center">
        <div className="flex size-16 shrink-0 self-center items-center justify-center overflow-hidden rounded-md bg-muted text-muted-foreground sm:self-auto">
          {previewURL ? (
            <img alt="" className="size-full object-cover" src={previewURL} />
          ) : (
            <ImageIcon className="size-5" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              className="h-auto min-h-11 w-full max-w-full whitespace-normal sm:w-auto"
              disabled={disabled}
              onClick={() => inputRef.current?.click()}
            >
              <Upload data-icon="inline-start" />
              {t("mediaLibrary.chooseImage")}
            </Button>
              {onSearch ?
                  <Button type="button" variant="outline" className="min-h-11 w-full sm:w-auto" disabled={disabled}
                          onClick={onSearch}><Search aria-hidden="true"/>{t("mediaImages.title")}</Button> : null}
            {onCapture ? (
              <Button
                type="button"
                variant="outline"
                className="h-auto min-h-11 w-full max-w-full whitespace-normal sm:w-auto"
                disabled={disabled || capturePending}
                onClick={onCapture}
              >
                {capturePending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <Camera data-icon="inline-start" />
                )}
                {t("mediaLibrary.captureVideoScreenshot")}
              </Button>
            ) : null}
          </div>
          {file ? (
            <div className="mt-2 truncate text-xs text-muted-foreground" title={file.name}>
              {file.name}
            </div>
          ) : null}
        </div>
      </div>
    </Field>
  )
}

interface MediaItemMetadataDialogProps {
  item: MediaLibraryItem | null
  open: boolean
  onClose: () => void
  onSaved: (item: MediaLibraryItem) => void
}

export function MediaItemMetadataDialog({
  item,
  open,
  onClose,
  onSaved,
}: MediaItemMetadataDialogProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const [sortName, setSortName] = useState("")
  const [tmdbID, setTMDBID] = useState("")
  const [tvdbID, setTVDBID] = useState("")
  const [metadataSource, setMetadataSource] = useState<MetadataSourceMode>("auto")
  const [genres, setGenres] = useState("")
  const [productionYear, setProductionYear] = useState("")
  const [premiereDate, setPremiereDate] = useState("")
  const [overview, setOverview] = useState("")
  const [rating, setRating] = useState("")
  const [runtimeMinutes, setRuntimeMinutes] = useState("")
  const [imageSourceURL, setImageSourceURL] = useState("")
  const [backdropSourceURL, setBackdropSourceURL] = useState("")
  const [imagePreviewURL, setImagePreviewURL] = useState("")
  const [backdropPreviewURL, setBackdropPreviewURL] = useState("")
  const [imageFile, setImageFile] = useState<File | null>(null)
  const [backdropFile, setBackdropFile] = useState<File | null>(null)
  const [seasonNumber, setSeasonNumber] = useState("")
  const [episodeNumber, setEpisodeNumber] = useState("")
  const [album, setAlbum] = useState("")
  const [albumArtist, setAlbumArtist] = useState("")
  const [artists, setArtists] = useState("")
  const [trackNumber, setTrackNumber] = useState("")
  const [discNumber, setDiscNumber] = useState("")
  const [lyrics, setLyrics] = useState("")
  const [translatedLyrics, setTranslatedLyrics] = useState("")
  const [error, setError] = useState("")
  const [searchOpen, setSearchOpen] = useState(false)
    const [imageSearchType, setImageSearchType] = useState<"primary" | "backdrop" | null>(null)
    const [primarySelection, setPrimarySelection] = useState<MediaImageSelection | null>(null)
    const [backdropSelection, setBackdropSelection] = useState<MediaImageSelection | null>(null)
  const originalTMDBID = useRef("")
  const originalTVDBID = useRef("")
  const originalSource = useRef<MetadataSourceMode>("auto")
  const pendingRefresh = useRef(false)

  useEffect(() => {
    if (!open || !item) return
    setName(item.name)
    setSortName(item.sort_name)
    setTMDBID(item.tmdb_id ? String(item.tmdb_id) : "")
    setTVDBID(item.tvdb_id ? String(item.tvdb_id) : "")
    setMetadataSource(item.metadata_source ?? "auto")
    originalTMDBID.current = item.tmdb_id ? String(item.tmdb_id) : ""
    originalTVDBID.current = item.tvdb_id ? String(item.tvdb_id) : ""
    originalSource.current = item.metadata_source ?? "auto"
    pendingRefresh.current = false
    setGenres(item.genres?.join(", ") ?? "")
    setProductionYear(item.production_year ? String(item.production_year) : "")
    setPremiereDate(item.premiere_date ?? "")
    setOverview(item.overview ?? "")
    setRating(item.community_rating ? String(item.community_rating) : "")
    setRuntimeMinutes(runtimeToMinutes(item.runtime_ticks))
    setImageSourceURL(item.image_source_url ?? "")
    setBackdropSourceURL(item.backdrop_source_url ?? "")
    setImagePreviewURL(item.image_url ?? "")
    setBackdropPreviewURL(item.backdrop_url ?? "")
    setImageFile(null)
    setBackdropFile(null)
    setSeasonNumber(item.season_number === undefined ? "" : String(item.season_number))
    setEpisodeNumber(item.episode_number === undefined ? "" : String(item.episode_number))
    setAlbum(item.album ?? "")
    setAlbumArtist(item.album_artist ?? "")
    setArtists(item.artists?.join(", ") ?? "")
    setTrackNumber(item.track_number === undefined ? "" : String(item.track_number))
    setDiscNumber(item.disc_number === undefined ? "" : String(item.disc_number))
    setLyrics(item.lyrics ?? "")
    setTranslatedLyrics(item.translated_lyrics ?? "")
    setError("")
    setSearchOpen(false)
      setImageSearchType(null)
      setPrimarySelection(null)
      setBackdropSelection(null)
  }, [item, open])

  const imageFilePreviewURL = useFilePreview(imageFile)
  const backdropFilePreviewURL = useFilePreview(backdropFile)
    const displayedImageURL = imageFilePreviewURL || primarySelection?.image.thumbnail_url || imagePreviewURL
    const displayedBackdropURL = backdropFilePreviewURL || backdropSelection?.image.thumbnail_url || backdropPreviewURL

  const saveMutation = useMutation({
    mutationFn: async ({
      payload,
      refreshRequest,
      primaryImage,
      backdropImage,
                           primaryRemote,
                           backdropRemote,
    }: {
      payload: MediaLibraryMetadataInput
      refreshRequest: MetadataRefreshRequest | null
      primaryImage: File | null
      backdropImage: File | null
        primaryRemote: MediaImageSelection | null
        backdropRemote: MediaImageSelection | null
    }) => {
      let saved = await updateMediaLibraryItemMetadata(item?.id ?? 0, payload)
      originalTMDBID.current = saved.tmdb_id ? String(saved.tmdb_id) : ""
      originalTVDBID.current = saved.tvdb_id ? String(saved.tvdb_id) : ""
      originalSource.current = saved.metadata_source ?? "auto"
      pendingRefresh.current = refreshRequest !== null
      if (refreshRequest) {
        saved = await refreshMediaLibraryItemMetadata(saved.id, refreshRequest)
        pendingRefresh.current = false
      }
        if (primaryRemote) saved = await selectMediaLibraryItemImage(saved.id, primaryRemote)
        if (backdropRemote) saved = await selectMediaLibraryItemImage(saved.id, backdropRemote)
      if (primaryImage) {
        saved = await uploadMediaLibraryItemImage(saved.id, "primary", primaryImage)
      }
      if (backdropImage) {
        saved = await uploadMediaLibraryItemImage(saved.id, "backdrop", backdropImage)
      }
      return saved
    },
    onSuccess: (saved) => {
      queryClient.setQueryData(["media-library-item", saved.id], saved)
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-detail-seasons"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-detail-episodes"] })
      toast.success(t("mediaLibrary.metadataSaved"))
      setImageFile(null)
      setBackdropFile(null)
      onSaved(saved)
      onClose()
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ManagerApiError && mutationError.message
          ? mutationError.message
          : t("mediaLibrary.metadataSaveFailed"),
      )
    },
  })

  const captureMutation = useMutation({
    mutationFn: () => captureMediaLibraryItemPrimaryImage(item?.id ?? 0),
    onSuccess: (saved) => {
      queryClient.setQueryData(["media-library-item", saved.id], saved)
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-detail-seasons"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-detail-episodes"] })
        setPrimarySelection(null)
      setImageSourceURL(saved.image_source_url ?? "")
      setImagePreviewURL(saved.image_url ?? "")
      setImageFile(null)
      toast.success(t("mediaLibrary.videoScreenshotCaptured"))
      onSaved(saved)
    },
    onError: (captureError) => {
      toast.error(
        captureError instanceof ManagerApiError && captureError.message
          ? captureError.message
          : t("mediaLibrary.videoScreenshotCaptureFailed"),
      )
    },
  })

  const isSeasonOrEpisode = item?.kind === "season" || item?.kind === "episode"
  const isEpisode = item?.kind === "episode"
  const isMusic = ["music_artist", "music_album", "audio"].includes(item?.kind ?? "")
  const isAudio = item?.kind === "audio"
  const canAutoRefreshMetadata = item?.kind === "movie" || item?.kind === "series"
  const canCaptureVideoScreenshot = ["movie", "series", "season", "episode"].includes(item?.kind ?? "")

  function applySearchResult(
    result: {
    id: number
    source?: "tmdb" | "tvdb"
    title?: string
    name?: string
    overview?: string
    release_date?: string
    first_air_date?: string
    vote_average?: number
    poster_url?: string
    },
    close = true,
  ) {
    if (result.source === "tvdb") setTVDBID(String(result.id))
    else setTMDBID(String(result.id))
    setMetadataSource(result.source ?? "tmdb")
    const title = (result.title || result.name || "").trim()
    if (title) {
      setName(title)
      setSortName(title)
    }
    if (result.overview) setOverview(result.overview)
    if (result.release_date || result.first_air_date) {
      setPremiereDate(result.release_date || result.first_air_date || "")
    }
    if (result.vote_average) setRating(String(Math.round(result.vote_average * 10) / 10))
    if (result.poster_url) {
      setImageSourceURL(result.poster_url)
      setImagePreviewURL(result.poster_url)
      setImageFile(null)
    }
    if (close) setSearchOpen(false)
  }

  function applyEpisodeSearch(selection: MediaEpisodePickerSelection) {
    const detail = item?.kind === "season" ? selection.season : selection.episode
    if (detail.source === "tvdb") setTVDBID(String(detail.id))
    else setTMDBID(String(detail.id))
    setName(detail.name)
    setSortName(detail.name)
    setOverview(detail.overview ?? "")
    setPremiereDate(detail.air_date ?? "")
    setRating(detail.vote_average ? String(Math.round(detail.vote_average * 10) / 10) : "")
    if ("runtime" in detail && detail.runtime) setRuntimeMinutes(String(detail.runtime))
    if ("still_url" in detail && detail.still_url) {
      setImageSourceURL(detail.still_url)
      setImagePreviewURL(detail.still_url)
      setImageFile(null)
    }
    if ("poster_url" in detail && detail.poster_url) {
      setImageSourceURL(detail.poster_url)
      setImagePreviewURL(detail.poster_url)
      setImageFile(null)
    }
    setSeasonNumber(String(detail.season_number))
    if ("episode_number" in detail) setEpisodeNumber(String(detail.episode_number))
    setSearchOpen(false)
  }

  function applyMusicSearch(
    track: {
      name: string
      artists: string[]
      album: string
      track_number?: number
      disc_number?: number
    },
    detail: { cover_url?: string; lyrics?: string; translated_lyrics?: string },
  ) {
    setName(track.name)
    setSortName(track.name)
    setAlbum(track.album)
    setAlbumArtist(track.artists[0] ?? "")
    setArtists(track.artists.join(", "))
    if (track.track_number) setTrackNumber(String(track.track_number))
    if (track.disc_number) setDiscNumber(String(track.disc_number))
    setImageSourceURL(detail.cover_url ?? "")
    setImagePreviewURL(detail.cover_url ?? "")
    setImageFile(null)
    setLyrics(detail.lyrics ?? "")
    setTranslatedLyrics(detail.translated_lyrics ?? "")
    setSearchOpen(false)
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const nextTMDBID = nullableInteger(tmdbID)
      const payload: MediaLibraryMetadataInput = {
        name: requiredText(name),
        sort_name: sortName.trim(),
        tmdb_id: nextTMDBID,
        tvdb_id: nullableInteger(tvdbID),
        metadata_source: metadataSource,
        imdb_id: item?.imdb_id ?? "",
        genres: splitValues(genres),
        season_number: nullableInteger(seasonNumber),
        episode_number: nullableInteger(episodeNumber),
        production_year: nullableInteger(productionYear),
        premiere_date: premiereDate.trim(),
        overview: overview.trim(),
        community_rating: nullableDecimal(rating),
        runtime_ticks: minutesToTicks(runtimeMinutes),
        image_source_url: imageSourceURL.trim(),
        backdrop_source_url: backdropSourceURL.trim(),
        album: album.trim(),
        album_artist: albumArtist.trim(),
        artists: splitValues(artists),
        track_number: nullableInteger(trackNumber),
        disc_number: nullableInteger(discNumber),
        lyrics: lyrics.trim(),
        translated_lyrics: translatedLyrics.trim(),
      }
      const needsRefresh = canAutoRefreshMetadata && (
        String(nextTMDBID ?? "") !== originalTMDBID.current ||
        String(nullableInteger(tvdbID) ?? "") !== originalTVDBID.current ||
        metadataSource !== originalSource.current || pendingRefresh.current
      )
      saveMutation.mutate({
        payload,
        refreshRequest: needsRefresh ? { metadata_source: metadataSource } : null,
        primaryImage: imageFile,
        backdropImage: backdropFile,
          primaryRemote: primarySelection,
          backdropRemote: backdropSelection,
      })
      setError("")
    } catch {
      setError(t("mediaLibrary.metadataInvalid"))
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && !saveMutation.isPending && !captureMutation.isPending && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-3xl">
        <form className="contents" onSubmit={handleSubmit}>
          <DialogHeader className="pr-10">
            <DialogTitle>{t("mediaLibrary.editMetadata")}</DialogTitle>
          </DialogHeader>
          <DialogBody className="min-w-0 pr-1">
            <FieldGroup>
              <div className="grid gap-5 sm:grid-cols-2">
                <Field data-invalid={Boolean(error)}>
                  <FieldLabel htmlFor="media-metadata-name">
                    {t("mediaLibrary.metadataTitle")}
                  </FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      id="media-metadata-name"
                      value={name}
                      maxLength={300}
                      required
                      onChange={(event) => setName(event.target.value)}
                    />
                    {isAudio ? (
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        aria-label={t("mediaLibrary.searchMusic")}
                        title={t("mediaLibrary.searchMusic")}
                        disabled={saveMutation.isPending}
                        onClick={() => setSearchOpen(true)}
                      >
                        <Search />
                      </Button>
                    ) : null}
                  </div>
                </Field>
                <Field>
                  <FieldLabel htmlFor="media-metadata-sort-name">
                    {t("mediaLibrary.sortName")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-sort-name"
                    value={sortName}
                    maxLength={300}
                    onChange={(event) => setSortName(event.target.value)}
                  />
                </Field>
              </div>

              {!isMusic ? (
                <div className="grid min-w-0 gap-5 rounded-lg border border-border bg-muted/20 p-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="media-metadata-tmdb-id">
                      {t("mediaLibrary.tmdbID")}
                    </FieldLabel>
                    <Input
                      id="media-metadata-tmdb-id"
                      inputMode="numeric"
                      type="number"
                      min={1}
                      value={tmdbID}
                      disabled={saveMutation.isPending || captureMutation.isPending}
                      onChange={(event) => setTMDBID(event.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="media-metadata-tvdb-id">{t("mediaLibrary.tvdbID")}</FieldLabel>
                    <Input
                      id="media-metadata-tvdb-id"
                      type="number"
                      inputMode="numeric"
                      min={1}
                      value={tvdbID}
                      disabled={saveMutation.isPending || captureMutation.isPending}
                      onChange={(event) => setTVDBID(event.target.value)}
                    />
                  </Field>
                  <div className="grid min-w-0 items-end gap-3 sm:col-span-2 sm:grid-cols-[minmax(0,1fr)_auto]">
                    {canAutoRefreshMetadata ? (
                      <Field className="min-w-0">
                        <FieldLabel htmlFor="media-metadata-source">{t("mediaLibrary.metadataSource")}</FieldLabel>
                        <Select
                          value={metadataSource}
                          disabled={saveMutation.isPending || captureMutation.isPending}
                          onValueChange={(value) => {
                            if (value === "auto" || value === "tmdb" || value === "tvdb") setMetadataSource(value)
                          }}
                        >
                          <SelectTrigger id="media-metadata-source" className="w-full min-w-0"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            <SelectItem value="auto">{t("mediaLibrary.sourceAuto")}</SelectItem>
                            <SelectItem value="tmdb">TMDB</SelectItem>
                            <SelectItem value="tvdb">TVDB</SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>
                    ) : null}
                    <Button
                      type="button"
                      variant="outline"
                      className={canAutoRefreshMetadata ? "w-full sm:w-auto" : "w-full sm:col-span-2 sm:w-auto sm:justify-self-end"}
                      disabled={saveMutation.isPending || captureMutation.isPending}
                      onClick={() => setSearchOpen(true)}
                    >
                      <Search aria-hidden="true" data-icon="inline-start" />
                      {t("mediaLibrary.searchMetadata")}
                    </Button>
                  </div>
                </div>
              ) : null}

              <Field>
                <FieldLabel htmlFor="media-metadata-overview">
                  {t("mediaLibrary.overview")}
                </FieldLabel>
                <Textarea
                  id="media-metadata-overview"
                  value={overview}
                  maxLength={20_000}
                  onChange={(event) => setOverview(event.target.value)}
                />
              </Field>

              <div className="grid gap-5 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="media-metadata-genres">
                    {t("mediaLibrary.genres")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-genres"
                    value={genres}
                    onChange={(event) => setGenres(event.target.value)}
                    placeholder={t("mediaLibrary.commaSeparated")}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="media-metadata-premiere-date">
                    {t("mediaLibrary.premiereDate")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-premiere-date"
                    type="date"
                    value={premiereDate}
                    onChange={(event) => setPremiereDate(event.target.value)}
                  />
                </Field>
              </div>

              <div className="grid gap-5 sm:grid-cols-3">
                <Field>
                  <FieldLabel htmlFor="media-metadata-year">
                    {t("mediaLibrary.productionYear")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-year"
                    inputMode="numeric"
                    type="number"
                    min={1}
                    max={9999}
                    value={productionYear}
                    onChange={(event) => setProductionYear(event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="media-metadata-rating">
                    {t("mediaLibrary.communityRating")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-rating"
                    inputMode="decimal"
                    type="number"
                    min={0}
                    max={10}
                    step="0.1"
                    value={rating}
                    onChange={(event) => setRating(event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="media-metadata-runtime">
                    {t("mediaLibrary.runtimeMinutes")}
                  </FieldLabel>
                  <Input
                    id="media-metadata-runtime"
                    inputMode="decimal"
                    type="number"
                    min={0}
                    step="0.1"
                    value={runtimeMinutes}
                    onChange={(event) => setRuntimeMinutes(event.target.value)}
                  />
                </Field>
              </div>

              <div className={isMusic ? "grid gap-5" : "grid gap-5 sm:grid-cols-2"}>
                <ImageUploadField
                  id="media-metadata-primary-image"
                  label={t(isMusic ? "mediaLibrary.coverImage" : "mediaLibrary.posterImage")}
                  previewURL={displayedImageURL}
                  file={imageFile}
                  disabled={saveMutation.isPending || captureMutation.isPending}
                  capturePending={captureMutation.isPending}
                  onSearch={!isMusic ? () => setImageSearchType("primary") : undefined}
                  onCapture={canCaptureVideoScreenshot ? () => captureMutation.mutate() : undefined}
                  onChange={(file) => {
                      setPrimarySelection(null)
                    setImageSourceURL("")
                    setImageFile(file)
                  }}
                />
                {!isMusic ? (
                  <ImageUploadField
                    id="media-metadata-backdrop-image"
                    label={t("mediaLibrary.backdropImage")}
                    previewURL={displayedBackdropURL}
                    file={backdropFile}
                    disabled={saveMutation.isPending || captureMutation.isPending}
                    onSearch={() => setImageSearchType("backdrop")}
                    onChange={(file) => {
                        setBackdropSelection(null)
                      setBackdropSourceURL("")
                      setBackdropFile(file)
                    }}
                  />
                ) : null}
              </div>

              {isSeasonOrEpisode ? (
                <div className="grid gap-5 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="media-metadata-season-number">
                      {t("mediaLibrary.seasonNumber")}
                    </FieldLabel>
                    <Input
                      id="media-metadata-season-number"
                      inputMode="numeric"
                      type="number"
                      min={0}
                      value={seasonNumber}
                      onChange={(event) => setSeasonNumber(event.target.value)}
                    />
                  </Field>
                  {isEpisode ? (
                    <Field>
                      <FieldLabel htmlFor="media-metadata-episode-number">
                        {t("mediaLibrary.episodeNumber")}
                      </FieldLabel>
                      <Input
                        id="media-metadata-episode-number"
                        inputMode="numeric"
                        type="number"
                        min={0}
                        value={episodeNumber}
                        onChange={(event) => setEpisodeNumber(event.target.value)}
                      />
                    </Field>
                  ) : null}
                </div>
              ) : null}

              {isMusic ? (
                <>
                  <div className="grid gap-5 sm:grid-cols-2">
                    <Field>
                      <FieldLabel htmlFor="media-metadata-album">
                        {t("mediaLibrary.album")}
                      </FieldLabel>
                      <Input
                        id="media-metadata-album"
                        value={album}
                        maxLength={300}
                        onChange={(event) => setAlbum(event.target.value)}
                      />
                    </Field>
                    <Field>
                      <FieldLabel htmlFor="media-metadata-album-artist">
                        {t("mediaLibrary.albumArtist")}
                      </FieldLabel>
                      <Input
                        id="media-metadata-album-artist"
                        value={albumArtist}
                        maxLength={300}
                        onChange={(event) => setAlbumArtist(event.target.value)}
                      />
                    </Field>
                  </div>
                  <Field>
                    <FieldLabel htmlFor="media-metadata-artists">
                      {t("mediaLibrary.artists")}
                    </FieldLabel>
                    <Input
                      id="media-metadata-artists"
                      value={artists}
                      onChange={(event) => setArtists(event.target.value)}
                      placeholder={t("mediaLibrary.commaSeparated")}
                    />
                  </Field>
                  {isAudio ? (
                    <>
                      <div className="grid gap-5 sm:grid-cols-2">
                        <Field>
                          <FieldLabel htmlFor="media-metadata-track-number">
                            {t("mediaLibrary.trackNumber")}
                          </FieldLabel>
                          <Input
                            id="media-metadata-track-number"
                            inputMode="numeric"
                            type="number"
                            min={0}
                            value={trackNumber}
                            onChange={(event) => setTrackNumber(event.target.value)}
                          />
                        </Field>
                        <Field>
                          <FieldLabel htmlFor="media-metadata-disc-number">
                            {t("mediaLibrary.discNumber")}
                          </FieldLabel>
                          <Input
                            id="media-metadata-disc-number"
                            inputMode="numeric"
                            type="number"
                            min={0}
                            value={discNumber}
                            onChange={(event) => setDiscNumber(event.target.value)}
                          />
                        </Field>
                      </div>
                      <Field>
                        <FieldLabel htmlFor="media-metadata-lyrics">
                          {t("mediaLibrary.lyrics")}
                        </FieldLabel>
                        <Textarea
                          id="media-metadata-lyrics"
                          value={lyrics}
                          maxLength={500_000}
                          className="min-h-40 font-mono text-xs leading-6"
                          onChange={(event) => setLyrics(event.target.value)}
                        />
                      </Field>
                      <Field>
                        <FieldLabel htmlFor="media-metadata-translated-lyrics">
                          {t("mediaLibrary.translatedLyrics")}
                        </FieldLabel>
                        <Textarea
                          id="media-metadata-translated-lyrics"
                          value={translatedLyrics}
                          maxLength={500_000}
                          className="min-h-40 font-mono text-xs leading-6"
                          onChange={(event) => setTranslatedLyrics(event.target.value)}
                        />
                      </Field>
                    </>
                  ) : null}
                </>
              ) : null}

              {error ? <FieldError role="alert" className="break-words">{error}</FieldError> : null}
            </FieldGroup>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={saveMutation.isPending || captureMutation.isPending}>
              {t("settings.cancel")}
            </Button>
            <Button type="submit" disabled={saveMutation.isPending || captureMutation.isPending}>
              {saveMutation.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}
              {t("mediaLibrary.saveMetadata")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
        {imageSearchType && item && open ? <MediaImagePicker
            item={{
                ...item, name, tmdb_id: Number(tmdbID) || undefined, tvdb_id: Number(tvdbID) || undefined,
                season_number: seasonNumber === "" ? undefined : Number(seasonNumber),
                episode_number: episodeNumber === "" ? undefined : Number(episodeNumber)
            }}
            imageType={imageSearchType}
            onClose={() => setImageSearchType(null)}
            onSelect={(selection) => {
                if (imageSearchType === "primary") {
                    setPrimarySelection(selection);
                    setImageFile(null)
                } else {
                    setBackdropSelection(selection);
                    setBackdropFile(null)
                }
                setImageSearchType(null)
            }}
        /> : null}
      <Dialog open={searchOpen} onOpenChange={setSearchOpen}>
        <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden sm:max-w-2xl">
          <DialogHeader className="pr-10">
            <DialogTitle>
              {t(isAudio ? "mediaLibrary.searchMusic" : "mediaLibrary.searchMetadata")}
            </DialogTitle>
          </DialogHeader>
          <DialogBody className="min-w-0 pr-1">
            {isAudio ? (
              <MusicPicker
                initialQuery={musicSearchQuery(name, albumArtist, artists)}
                onSelect={applyMusicSearch}
              />
            ) : (
              <MediaPicker
                source={metadataSource}
                movieOnly={item?.kind === "movie"}
                tvOnly={isSeasonOrEpisode || item?.kind === "series"}
                onMovieSelect={applySearchResult}
                onTVSelect={applySearchResult}
                onEpisodeSelect={(selection) => selection && applyEpisodeSearch(selection)}
              />
            )}
          </DialogBody>
        </DialogContent>
      </Dialog>
    </Dialog>
  )
}

function requiredText(value: string) {
  const trimmed = value.trim()
  if (!trimmed) throw new Error("name is required")
  return trimmed
}

function nullableInteger(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return null
  if (!/^\d+$/.test(trimmed)) throw new Error("invalid integer")
  const parsed = Number(trimmed)
  if (!Number.isSafeInteger(parsed)) throw new Error("invalid integer")
  return parsed
}

function nullableDecimal(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 0
  const parsed = Number(trimmed)
  if (!Number.isFinite(parsed)) throw new Error("invalid decimal")
  return parsed
}

function minutesToTicks(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 0
  const minutes = Number(trimmed)
  if (!Number.isFinite(minutes) || minutes < 0) throw new Error("invalid runtime")
  const ticks = Math.round(minutes * ticksPerMinute)
  if (!Number.isSafeInteger(ticks)) throw new Error("invalid runtime")
  return ticks
}

function runtimeToMinutes(ticks?: number) {
  if (!ticks) return ""
  return String(Math.round((ticks / ticksPerMinute) * 10) / 10)
}

function splitValues(value: string) {
  return value.split(",").map((item) => item.trim()).filter(Boolean)
}

function musicSearchQuery(name: string, albumArtist: string, artists: string) {
  const title = name.trim()
  const artist = albumArtist.trim() || splitValues(artists)[0] || ""
  if (!artist || artist === "Unknown Artist") return title
  return `${title} ${artist}`.trim()
}
