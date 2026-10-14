import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  MediaPlayer,
  MediaProvider,
  type AudioMimeType,
  type MediaPlayerInstance,
  type VideoMimeType,
} from "@vidstack/react"
import {
  DefaultVideoLayout,
  defaultLayoutIcons,
  type DefaultLayoutTranslations,
} from "@vidstack/react/player/layouts/default"
import "@vidstack/react/player/styles/default/theme.css"
import "@vidstack/react/player/styles/default/layouts/audio.css"
import "@vidstack/react/player/styles/default/layouts/video.css"
import {
  ArrowLeft,
  ChevronDown,
  ChevronUp,
  Clock3,
  FileVideo2,
  ListMusic,
  LibraryBig,
  MoreHorizontal,
  Music2,
  Pause,
  Play,
  Repeat,
  Repeat1,
  RotateCcw,
  Shuffle,
  SkipForward,
  TriangleAlert,
  Volume2,
  VolumeX,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  getMediaLibraryItem,
  getNextMediaLibraryEpisode,
  getMediaLibraryStreamURL,
  listMediaLibraryItems,
  type MediaLibraryItem,
  type MediaLibraryPlaybackUpdate,
  updateMediaLibraryPlayback,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { Spinner } from "@/components/ui/spinner"
import { displayLyrics } from "./mediaItemPresentation"

const TICKS_PER_SECOND = 10_000_000
const PROGRESS_SAVE_INTERVAL = 15_000
const MUSIC_QUEUE_PAGE_SIZE = 200

type PlaybackMode = "sequential" | "repeat-all" | "repeat-one" | "shuffle"
type AudioView = "artwork" | "lyrics" | "queue"

interface StoredAudioPreferences {
  mode: PlaybackMode
  volume: number
  view: AudioView
}

const audioMimeTypes: Record<string, AudioMimeType> = {
  aac: "audio/mpeg",
  flac: "audio/flac",
  m4a: "audio/mpeg",
  mp3: "audio/mpeg",
  oga: "audio/ogg",
  ogg: "audio/ogg",
  opus: "audio/ogg",
  wav: "audio/mpeg",
}

const videoMimeTypes: Record<string, VideoMimeType> = {
  m4v: "video/mp4",
  mov: "video/mp4",
  mp4: "video/mp4",
  mpeg: "video/mpeg",
  mpg: "video/mpeg",
  ogv: "video/ogg",
  webm: "video/webm",
}

const zhPlayerTranslations: Partial<DefaultLayoutTranslations> = {
  Audio: "音频",
  Captions: "字幕",
  "Enter Fullscreen": "进入全屏",
  "Enter PiP": "进入画中画",
  "Exit Fullscreen": "退出全屏",
  "Exit PiP": "退出画中画",
  Loop: "循环",
  Mute: "静音",
  Normal: "正常",
  Pause: "暂停",
  Play: "播放",
  Playback: "播放",
  Quality: "画质",
  Replay: "重新播放",
  "Seek Backward": "快退",
  "Seek Forward": "快进",
  Settings: "设置",
  Speed: "速度",
  Unmute: "取消静音",
  Volume: "音量",
}

export default function MediaPlaybackPage({
  itemID,
  onBack,
  onPlayItem,
}: {
  itemID: number
  onBack: () => void
  onPlayItem?: (itemID: number) => void
}) {
  const { language, locale, t } = useI18n()
  const queryClient = useQueryClient()
  const playerRef = useRef<MediaPlayerInstance>(null)
  const currentTimeRef = useRef(0)
  const finishedRef = useRef(false)
  const resumedRef = useRef(false)
  const autoplayAttemptedRef = useRef(false)
  const [playerError, setPlayerError] = useState(false)
  const [retryNonce, setRetryNonce] = useState(0)
  const [nextEpisode, setNextEpisode] = useState<MediaLibraryItem | null>(null)
  const [nextEpisodeCountdown, setNextEpisodeCountdown] = useState(8)

  useEffect(() => {
    currentTimeRef.current = 0
    finishedRef.current = false
    resumedRef.current = false
    autoplayAttemptedRef.current = false
    setPlayerError(false)
	setNextEpisode(null)
	setNextEpisodeCountdown(8)
  }, [itemID])

  const itemQuery = useQuery({
    queryKey: ["media-library-item", itemID],
    queryFn: () => getMediaLibraryItem(itemID),
  })

  const progressMutation = useMutation({
	scope: { id: `media-library-playback-${itemID}` },
    mutationFn: (payload: MediaLibraryPlaybackUpdate) =>
      updateMediaLibraryPlayback(itemID, payload),
    onSuccess: (userData) => {
      queryClient.setQueryData<MediaLibraryItem>(
        ["media-library-item", itemID],
        (current) => current && { ...current, user_data: userData },
      )
	  void queryClient.invalidateQueries({ queryKey: ["media-library-continue-watching"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-home"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
    },
  })

  const persistProgress = useCallback(
    (stopped: boolean) => {
	  if (finishedRef.current && !stopped) return
      const seconds = currentTimeRef.current
      if (!Number.isFinite(seconds) || seconds <= 0) return
      progressMutation.mutate({
        position_ticks: Math.round(seconds * TICKS_PER_SECOND),
        stopped,
      })
    },
    [progressMutation],
  )

  const playNextEpisode = useCallback(() => {
	if (!nextEpisode || !onPlayItem) return
	setNextEpisode(null)
	onPlayItem(nextEpisode.id)
  }, [nextEpisode, onPlayItem])

  useEffect(() => {
	if (!nextEpisode) return
	const timer = window.setTimeout(() => {
		if (nextEpisodeCountdown <= 1) {
			playNextEpisode()
			return
		}
		setNextEpisodeCountdown((current) => current - 1)
	}, 1000)
	return () => window.clearTimeout(timer)
  }, [nextEpisode, nextEpisodeCountdown, playNextEpisode])

  const finishVideo = useCallback(async () => {
	finishedRef.current = true
	const seconds = currentTimeRef.current
	if (!Number.isFinite(seconds) || seconds <= 0) return
	try {
		await progressMutation.mutateAsync({
			position_ticks: Math.round(seconds * TICKS_PER_SECOND),
			stopped: true,
		})
		const response = await getNextMediaLibraryEpisode(itemID)
		if (response.item) {
			setNextEpisodeCountdown(8)
			setNextEpisode(response.item)
		}
	} catch {
		// The normal ended state remains visible when persistence or lookup fails.
	}
  }, [itemID, progressMutation])

  useEffect(() => {
    const interval = window.setInterval(() => {
      if (!playerRef.current?.paused) persistProgress(false)
    }, PROGRESS_SAVE_INTERVAL)
    return () => window.clearInterval(interval)
  }, [persistProgress])

  useEffect(() => {
    const saveBeforeExit = () => {
      if (finishedRef.current || currentTimeRef.current <= 0) return
      void updateMediaLibraryPlayback(
        itemID,
        {
          position_ticks: Math.round(currentTimeRef.current * TICKS_PER_SECOND),
          stopped: true,
        },
        true,
      ).catch(() => undefined)
    }
    window.addEventListener("beforeunload", saveBeforeExit)
    return () => {
      window.removeEventListener("beforeunload", saveBeforeExit)
      saveBeforeExit()
    }
  }, [itemID])

  if (itemQuery.isLoading) {
    return (
      <PlaybackState>
        <Spinner className="size-6 text-emerald-300" />
        <p>{t("playback.loading")}</p>
      </PlaybackState>
    )
  }

  if (itemQuery.isError || !itemQuery.data) {
    return (
      <PlaybackState>
        <TriangleAlert className="size-7 text-amber-300" />
        <h1 className="text-lg font-semibold text-white">
          {t("playback.unavailable")}
        </h1>
        <p className="max-w-md text-center text-sm text-white/55">
          {t("playback.loadFailed")}
        </p>
        <div className="flex gap-3">
          <Button variant="outline" onClick={onBack}>
            <ArrowLeft data-icon="inline-start" />
            {t("playback.back")}
          </Button>
          <Button onClick={() => void itemQuery.refetch()}>
            {t("playback.retry")}
          </Button>
        </div>
      </PlaybackState>
    )
  }

  const item = itemQuery.data
  const isAudio = item.kind === "audio"
  if (!isMediaFormatSupported(item)) {
    return (
      <PlaybackState>
        <FileVideo2 className="size-8 text-amber-300" />
        <p className="max-w-md text-center text-sm text-white/55">{item.name}</p>
        <h1 className="text-lg font-semibold text-white">
          {t("playback.unavailable")}
        </h1>
        <p className="max-w-md text-center text-sm leading-6 text-white/55">
          {t("playback.unsupported")}
        </p>
        <Button variant="outline" onClick={onBack}>
          <ArrowLeft data-icon="inline-start" />
          {t("playback.back")}
        </Button>
      </PlaybackState>
    )
  }
  const poster = item.backdrop_url || item.image_url
  const source = {
    src: getMediaLibraryStreamURL(item.id),
    type: mediaMimeType(item),
  }
  const resumeSeconds = item.user_data.playback_position_ticks / TICKS_PER_SECOND

  if (isAudio) {
    return (
      <AudioPlaybackExperience
        item={item}
        source={{ ...source, type: source.type as AudioMimeType }}
        resumeSeconds={resumeSeconds}
        playerError={playerError}
        setPlayerError={setPlayerError}
        retryNonce={retryNonce}
        setRetryNonce={setRetryNonce}
        playerRef={playerRef}
        currentTimeRef={currentTimeRef}
        resumedRef={resumedRef}
        autoplayAttemptedRef={autoplayAttemptedRef}
        finishedRef={finishedRef}
        persistProgress={persistProgress}
        onBack={onBack}
        onPlayItem={onPlayItem ?? (() => undefined)}
      />
    )
  }

  function restartPlayback() {
    if (!playerRef.current) return
    playerRef.current.currentTime = 0
    currentTimeRef.current = 0
    finishedRef.current = false
    void playerRef.current.play().catch(() => setPlayerError(true))
  }

  return (
    <div className="min-h-dvh bg-[#070908] text-white">
      <header className="flex h-16 items-center justify-between border-b border-white/10 px-3 sm:px-6">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-11 text-white/75 hover:bg-white/10 hover:text-white"
          aria-label={t("playback.back")}
          onClick={onBack}
        >
          <ArrowLeft className="size-5" />
        </Button>
        <div className="min-w-0 px-3 text-center">
          <p className="truncate text-sm font-medium text-white/90">{item.name}</p>
          <p className="truncate text-xs text-white/45">{mediaSubtitle(item)}</p>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-11 text-white/75 hover:bg-white/10 hover:text-white"
              aria-label={t("playback.more")}
            >
              <MoreHorizontal className="size-5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuItem onSelect={restartPlayback}>
              <RotateCcw />
              {t("playback.restart")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onBack}>
              <LibraryBig />
              {t("playback.back")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>{t("playback.details")}</DropdownMenuLabel>
            <div className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-2 px-2 py-1.5 text-xs text-muted-foreground">
              <span>{t("playback.format")}</span>
              <span className="font-medium text-foreground">
                {(item.container || "-").toUpperCase()}
              </span>
              {item.runtime_ticks ? (
                <>
                  <span>{t("playback.runtime")}</span>
                  <span className="font-medium text-foreground">
                    {formatRuntime(item.runtime_ticks, locale)}
                  </span>
                </>
              ) : null}
            </div>
          </DropdownMenuContent>
        </DropdownMenu>
      </header>

      <main className="mx-auto flex w-full max-w-[1440px] flex-col px-3 py-4 sm:px-6 sm:py-7 lg:px-10">
        <div className={isAudio ? "mx-auto w-full max-w-3xl" : "w-full"}>
          {isAudio ? <AudioArtwork item={item} /> : null}
          <div className="relative overflow-hidden rounded-lg border border-white/10 bg-black shadow-2xl shadow-black/40">
            <MediaPlayer
              key={retryNonce}
              ref={playerRef}
              className={isAudio ? "media-playback-player audio" : "media-playback-player video aspect-video"}
              src={source}
              title={item.name}
              poster={isAudio ? undefined : poster}
              viewType={isAudio ? "audio" : "video"}
              streamType="on-demand"
              load="eager"
              preload="metadata"
              playsInline
              onCanPlay={() => {
                if (!resumedRef.current && resumeSeconds > 0 && playerRef.current) {
                  playerRef.current.currentTime = resumeSeconds
                  currentTimeRef.current = resumeSeconds
                  resumedRef.current = true
                }
                attemptAutoplay(playerRef, autoplayAttemptedRef, setPlayerError)
              }}
              onTimeUpdate={({ currentTime }) => {
                currentTimeRef.current = currentTime
              }}
              onPause={() => persistProgress(false)}
              onEnded={() => void finishVideo()}
              onError={() => setPlayerError(true)}
              onPlayFail={() => setPlayerError(true)}
            >
              <MediaProvider />
              <DefaultVideoLayout
                icons={defaultLayoutIcons}
                colorScheme="dark"
                playbackRates={[0.5, 0.75, 1, 1.25, 1.5, 2]}
                translations={language === "zh" ? zhPlayerTranslations : null}
              />
            </MediaPlayer>

            {playerError ? (
              <div className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-3 bg-black/90 px-6 text-center">
                <TriangleAlert className="size-8 text-amber-300" />
                <h2 className="text-lg font-semibold">{t("playback.unavailable")}</h2>
                <p className="max-w-md text-sm leading-6 text-white/55">
                  {t("playback.unsupported")}
                </p>
                <Button
                  variant="secondary"
                  onClick={() => {
                    setPlayerError(false)
                    resumedRef.current = false
                    autoplayAttemptedRef.current = false
                    setRetryNonce((current) => current + 1)
                  }}
                >
                  <RotateCcw data-icon="inline-start" />
                  {t("playback.retry")}
                </Button>
              </div>
            ) : null}

			{nextEpisode ? (
			  <div className="absolute inset-x-0 bottom-0 z-20 flex flex-col gap-3 border-t border-white/15 bg-black/90 p-4 backdrop-blur-sm sm:flex-row sm:items-center sm:p-5">
				<div className="min-w-0 flex-1">
				  <p className="text-xs font-medium text-emerald-300">{t("playback.nextEpisode")}</p>
				  <p className="mt-1 truncate text-sm font-semibold text-white">{nextEpisode.name}</p>
				  <p className="mt-1 text-xs tabular-nums text-white/55">
					{t("playback.playingNextIn", { seconds: nextEpisodeCountdown })}
				  </p>
				</div>
				<div className="flex gap-2">
				  <Button type="button" onClick={playNextEpisode}>
					<SkipForward data-icon="inline-start" />
					{t("playback.playNow")}
				  </Button>
				  <Button type="button" variant="secondary" onClick={() => setNextEpisode(null)}>
					{t("playback.cancelNext")}
				  </Button>
				</div>
			  </div>
			) : null}
          </div>

          <section className="grid gap-3 px-1 pb-6 pt-5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
            <div className="min-w-0">
              <h1 className="text-xl font-semibold sm:text-2xl">{item.name}</h1>
              <p className="mt-1 text-sm text-white/50">{mediaSubtitle(item)}</p>
            </div>
            <div className="flex flex-wrap gap-2 text-xs text-white/55">
              {item.container ? (
                <span className="inline-flex h-7 items-center gap-1.5 rounded border border-white/10 px-2.5">
                  <FileVideo2 className="size-3.5" />
                  {item.container.toUpperCase()}
                </span>
              ) : null}
              {item.runtime_ticks ? (
                <span className="inline-flex h-7 items-center gap-1.5 rounded border border-white/10 px-2.5">
                  <Clock3 className="size-3.5" />
                  {formatRuntime(item.runtime_ticks, locale)}
                </span>
              ) : null}
            </div>
          </section>
		  {isAudio ? <AudioLyrics item={item} /> : null}
        </div>
      </main>
    </div>
  )
}

export type AudioPlaybackItem = Pick<MediaLibraryItem, "id" | "library_id" | "name" | "path" | "image_url" | "artists" | "album" | "album_artist" | "lyrics" | "translated_lyrics" | "user_data">

export interface AudioPlaybackExperienceProps {
  item: AudioPlaybackItem
  source: { src: string; type: AudioMimeType }
  resumeSeconds: number
  playerError: boolean
  setPlayerError: (value: boolean) => void
  retryNonce: number
  setRetryNonce: React.Dispatch<React.SetStateAction<number>>
  playerRef: React.RefObject<MediaPlayerInstance>
  currentTimeRef: React.MutableRefObject<number>
  resumedRef: React.MutableRefObject<boolean>
  autoplayAttemptedRef: React.MutableRefObject<boolean>
  finishedRef: React.MutableRefObject<boolean>
  persistProgress: (stopped: boolean) => void
  onBack: () => void
  onPlayItem: (itemID: number) => void
  inline?: boolean
  onPlaybackStateChange?: (playing: boolean) => void
}

export function AudioPlaybackExperience({
  item,
  source,
  resumeSeconds,
  playerError,
  setPlayerError,
  retryNonce,
  setRetryNonce,
  playerRef,
  currentTimeRef,
  resumedRef,
  autoplayAttemptedRef,
  finishedRef,
  persistProgress,
  onBack,
  onPlayItem,
  inline = false,
  onPlaybackStateChange,
}: AudioPlaybackExperienceProps) {
  const { locale, t } = useI18n()
  const [mode, setMode] = useState<PlaybackMode>("sequential")
  const [view, setView] = useState<AudioView>("artwork")
  const [volume, setVolume] = useState(0.85)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [isPlaying, setIsPlaying] = useState(false)
  const [muted, setMuted] = useState(false)
  const [queueOpen, setQueueOpen] = useState(false)
  const [lyricsOpen, setLyricsOpen] = useState(false)
  const [activeLyric, setActiveLyric] = useState(0)
  const lyricsRef = useRef<HTMLDivElement>(null)
  const prefsKey = "openlist2emby.audio-player.preferences"

  useEffect(() => {
    setCurrentTime(0)
    setDuration(0)
    setIsPlaying(false)
    setPlayerError(false)
    resumedRef.current = false
    autoplayAttemptedRef.current = false
  }, [autoplayAttemptedRef, item.id, setPlayerError, source.src, resumedRef])

  useEffect(() => {
    try {
      const stored = JSON.parse(window.localStorage.getItem(prefsKey) ?? "null") as Partial<StoredAudioPreferences> | null
      if (stored?.mode) setMode(stored.mode)
      if (typeof stored?.volume === "number") setVolume(Math.min(1, Math.max(0, stored.volume)))
      if (stored?.view) setView(stored.view)
    } catch {
      // Ignore malformed local preferences.
    }
  }, [])

  useEffect(() => {
    window.localStorage.setItem(prefsKey, JSON.stringify({ mode, volume, view }))
  }, [mode, volume, view])

  const queueQuery = useInfiniteQuery({
    queryKey: ["music-playback-queue", item.library_id],
    queryFn: async ({ pageParam }) => {
      const page = await listMediaLibraryItems({
        libraryId: item.library_id,
        kinds: ["audio"],
        sortBy: "SortName",
        start: pageParam,
        limit: MUSIC_QUEUE_PAGE_SIZE,
      })
      return { items: Array.isArray(page.items) ? page.items : [], total: Number(page.total) || 0 }
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage, pages) => {
      const loaded = pages.reduce((total, page) => total + page.items.length, 0)
      return loaded < lastPage.total && lastPage.items.length > 0 ? loaded : undefined
    },
    enabled: !inline,
  })
  const queue = useMemo(
    () => queueQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [queueQuery.data?.pages],
  )
  const currentIndex = queue.findIndex((candidate) => candidate.id === item.id)
  const { fetchNextPage, hasNextPage, isFetchingNextPage } = queueQuery
  const lyrics = useMemo(() => mergeLyrics(item.lyrics, item.translated_lyrics), [item.lyrics, item.translated_lyrics])

  useEffect(() => {
    if (!inline && (currentIndex < 0 || (queueOpen && currentIndex > queue.length - 20)) && hasNextPage && !isFetchingNextPage) {
      void fetchNextPage()
    }
  }, [currentIndex, fetchNextPage, hasNextPage, inline, isFetchingNextPage, queue.length, queueOpen])

  useEffect(() => {
    const line = lyricsRef.current?.querySelector<HTMLElement>(`[data-lyric-index="${activeLyric}"]`)
    line?.scrollIntoView({ block: "center", behavior: "smooth" })
  }, [activeLyric])

  const selectTrack = useCallback((next: MediaLibraryItem) => {
    if (next.id !== item.id) onPlayItem(next.id)
  }, [item.id, onPlayItem])

  const playAdjacent = useCallback((direction: -1 | 1) => {
    if (inline) return
    if (queue.length === 0 || currentIndex < 0) return
    const nextIndex = currentIndex + direction
    if (nextIndex >= 0 && nextIndex < queue.length) {
      selectTrack(queue[nextIndex])
    } else if (direction > 0 && mode === "repeat-all") {
      selectTrack(queue[0])
    } else if (direction < 0 && mode === "repeat-all") {
      selectTrack(queue[queue.length - 1])
    }
  }, [currentIndex, inline, mode, queue, selectTrack])

  const playNext = useCallback(() => {
    if (mode === "repeat-one") {
      if (playerRef.current) {
        playerRef.current.currentTime = 0
        void playerRef.current.play().catch(() => setPlayerError(true))
      }
      return
    }
    if (inline) return
    if (mode === "shuffle" && queue.length > 1) {
      const candidates = queue.filter((candidate) => candidate.id !== item.id)
      selectTrack(candidates[Math.floor(Math.random() * candidates.length)])
      return
    }
    playAdjacent(1)
  }, [inline, item.id, mode, playAdjacent, playerRef, queue, selectTrack, setPlayerError])

  const poster = item.image_url
  const subtitle = [item.artists?.join(", "), item.album].filter(Boolean).join(" · ") || item.path
  const displayView = window.matchMedia("(min-width: 1024px)").matches ? "artwork" : view

  useEffect(() => {
    if (!("mediaSession" in navigator)) return
    navigator.mediaSession.metadata = new MediaMetadata({
      title: item.name,
      artist: item.artists?.join(", ") || item.album_artist || "",
      album: item.album || "",
      artwork: poster ? [{ src: poster }] : [],
    })
    const actions: Array<[MediaSessionAction, MediaSessionActionHandler | null]> = [
      ["play", () => void playerRef.current?.play()],
      ["pause", () => playerRef.current?.pause()],
      ["previoustrack", () => playAdjacent(-1)],
      ["nexttrack", playNext],
      ["seekbackward", () => { if (playerRef.current) playerRef.current.currentTime = Math.max(0, playerRef.current.currentTime - 10) }],
      ["seekforward", () => { if (playerRef.current) playerRef.current.currentTime = Math.min(duration, playerRef.current.currentTime + 10) }],
    ]
    for (const [action, handler] of actions) {
      try { navigator.mediaSession.setActionHandler(action, handler) } catch { /* Unsupported action. */ }
    }
    return () => {
      for (const [action] of actions) {
        try { navigator.mediaSession.setActionHandler(action, null) } catch { /* Unsupported action. */ }
      }
    }
  }, [duration, item.album, item.album_artist, item.artists, item.name, playAdjacent, playNext, playerRef, poster])

  useEffect(() => {
    onPlaybackStateChange?.(isPlaying)
  }, [isPlaying, onPlaybackStateChange])

  return (
    <div className={`relative overflow-hidden bg-[#070908] text-white ${inline ? "rounded-lg border border-white/10" : "min-h-dvh"}`}>
      {poster ? <div aria-hidden className="absolute inset-0 scale-110 bg-cover bg-center opacity-25 blur-3xl" style={{ backgroundImage: `url(${poster})` }} /> : null}
      <div aria-hidden className="absolute inset-0 bg-[linear-gradient(135deg,rgba(7,9,8,.88),rgba(7,9,8,.97)_65%)]" />
      <header className="relative z-10 flex h-16 items-center justify-between border-b border-white/10 px-3 sm:px-6">
        {inline ? <span className="size-11" /> : <Button type="button" variant="ghost" size="icon" className="size-11 text-white/75 hover:bg-white/10 hover:text-white" aria-label={t("playback.back")} onClick={onBack}><ArrowLeft className="size-5" /></Button>}
        <div className="min-w-0 px-3 text-center"><p className="truncate text-sm font-medium">{item.name}</p><p className="truncate text-xs text-white/45">{subtitle}</p></div>
        {inline ? <span className="size-11" /> : <Button type="button" variant="ghost" size="icon" className="size-11 text-white/75 hover:bg-white/10 hover:text-white" aria-label={t("playback.queue")} onClick={() => setQueueOpen((open) => !open)}><ListMusic className="size-5" /></Button>}
      </header>

      <main className={`relative z-10 mx-auto grid w-full gap-5 px-4 py-6 lg:grid-cols-[minmax(10rem,15rem)_minmax(0,1fr)] lg:items-center lg:gap-6 lg:px-6 ${inline ? "max-w-4xl" : "min-h-[calc(100dvh-4rem)] max-w-6xl lg:grid-cols-[minmax(20rem,28rem)_minmax(0,1fr)] lg:gap-12 lg:px-8"}`}>
        <section className={displayView === "lyrics" ? "hidden lg:block" : "flex flex-col items-center justify-center"}>
          <div className={`relative aspect-square w-full overflow-hidden rounded-md border border-white/15 bg-white/[0.06] shadow-2xl shadow-black/50 ${inline ? "max-w-48" : "max-w-[25rem]"}`}>
            {poster ? <img src={poster} alt={item.name} className="size-full object-cover" /> : <div className="flex size-full items-center justify-center"><Music2 className="size-20 text-white/25" strokeWidth={1.1} /></div>}
          </div>
          <div className={`mt-4 w-full text-center lg:text-left ${inline ? "max-w-48" : "max-w-[25rem]"}`}><h1 className="truncate text-xl font-semibold sm:text-2xl">{item.name}</h1><p className="mt-2 truncate text-sm text-white/55">{subtitle}</p></div>
        </section>

        <section className="min-w-0 rounded-lg border border-white/10 bg-black/20 p-4 backdrop-blur-xl sm:p-6">
          <div className="mb-4 flex items-center justify-between lg:hidden">
            <div className="flex gap-1 rounded-md bg-white/[0.06] p-1">
              {(inline ? (["artwork", "lyrics"] as AudioView[]) : (["artwork", "lyrics", "queue"] as AudioView[])).map((target) => <Button key={target} variant="ghost" size="sm" className={view === target ? "bg-white/10 text-white" : "text-white/50"} onClick={() => setView(target)}>{target === "artwork" ? <Music2 /> : target === "lyrics" ? <ListMusic /> : <LibraryBig />}</Button>)}
            </div>
          </div>
          {(!inline && (displayView === "queue" || queueOpen)) ? <QueuePanel items={queue} currentID={item.id} loading={queueQuery.isLoading} onSelect={selectTrack} loadingLabel={t("common.loading")} /> : displayView === "lyrics" || lyricsOpen || lyrics.length > 0 ? <LyricsPanel lyrics={lyrics} activeIndex={activeLyric} setActiveIndex={setActiveLyric} lyricsRef={lyricsRef} playerRef={playerRef} t={t} /> : <div className="flex min-h-[10rem] items-center justify-center text-center text-sm text-white/45">{t("playback.nowPlaying")}</div>}

          <MediaPlayer key={retryNonce} ref={playerRef} className="sr-only" src={source} title={item.name} viewType="audio" streamType="on-demand" load="eager" preload="metadata" playsInline volume={volume} muted={muted} onCanPlay={() => { if (!resumedRef.current && resumeSeconds > 0 && playerRef.current) { playerRef.current.currentTime = resumeSeconds; currentTimeRef.current = resumeSeconds; resumedRef.current = true } attemptAutoplay(playerRef, autoplayAttemptedRef, setPlayerError) }} onDurationChange={(next) => setDuration(Number(next))} onTimeUpdate={({ currentTime: next }) => { currentTimeRef.current = next; setCurrentTime(next); const index = lyrics.findIndex((line, index) => next >= line.time && (lyrics[index + 1] === undefined || next < lyrics[index + 1].time)); if (index >= 0) setActiveLyric(index) }} onPlay={() => setIsPlaying(true)} onPause={() => { setIsPlaying(false); persistProgress(false) }} onEnded={() => { finishedRef.current = true; persistProgress(true); playNext() }} onError={() => setPlayerError(true)}><MediaProvider /><span>audio controls</span></MediaPlayer>

          <div className="mt-5"><input aria-label={t("playback.progress")} type="range" min={0} max={Math.max(duration, 1)} step={0.1} value={Math.min(currentTime, duration || 0)} onChange={(event) => { const next = Number(event.target.value); setCurrentTime(next); if (playerRef.current) playerRef.current.currentTime = next }} className="w-full accent-emerald-300" /><div className="mt-1 flex justify-between text-xs text-white/45"><span>{formatSeconds(currentTime, locale)}</span><span>{formatSeconds(duration, locale)}</span></div></div>
          <div className="mt-4 flex items-center justify-center gap-1 sm:gap-3"><Button variant="ghost" size="icon" className={`size-11 text-white/60 hover:bg-white/10 hover:text-white ${inline ? "invisible" : ""}`} aria-label={t("playback.previous")} onClick={() => playAdjacent(-1)}><ChevronDown className="rotate-90" /></Button><Button variant="ghost" size="icon" className="size-11 text-white/60 hover:bg-white/10 hover:text-white" aria-label={t("playback.seekBackward")} onClick={() => { if (playerRef.current) playerRef.current.currentTime = Math.max(0, currentTime - 10) }}><span className="text-xs font-semibold">-10</span></Button><Button variant="secondary" size="icon" className="size-14 rounded-full bg-emerald-300 text-black hover:bg-emerald-200" aria-label={isPlaying ? t("playback.pause") : t("playback.play")} onClick={() => { if (!playerRef.current) return; if (isPlaying) playerRef.current.pause(); else void playerRef.current.play().catch(() => setPlayerError(true)) }}>{isPlaying ? <Pause /> : <Play className="ml-0.5" />}</Button><Button variant="ghost" size="icon" className="size-11 text-white/60 hover:bg-white/10 hover:text-white" aria-label={t("playback.seekForward")} onClick={() => { if (playerRef.current) playerRef.current.currentTime = Math.min(duration, currentTime + 10) }}><span className="text-xs font-semibold">+10</span></Button><Button variant="ghost" size="icon" className={`size-11 text-white/60 hover:bg-white/10 hover:text-white ${inline ? "invisible" : ""}`} aria-label={t("playback.next")} onClick={playNext}><ChevronUp className="rotate-90" /></Button></div>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-white/10 pt-4"><div className="flex items-center gap-1"><Button variant="ghost" size="icon" className={mode === "shuffle" ? "size-11 text-emerald-300" : "size-11 text-white/50"} aria-label={t("playback.shuffle")} onClick={() => setMode(mode === "shuffle" ? "sequential" : "shuffle")}><Shuffle /></Button><Button variant="ghost" size="icon" className={mode === "repeat-one" || mode === "repeat-all" ? "size-11 text-emerald-300" : "size-11 text-white/50"} aria-label={t("playback.repeat")} onClick={() => setMode(mode === "repeat-one" ? "sequential" : mode === "repeat-all" ? "repeat-one" : "repeat-all")}>{mode === "repeat-one" ? <Repeat1 /> : <Repeat />}</Button><Button variant="ghost" size="icon" className="size-11 text-white/50" aria-label={t("playback.lyrics")} onClick={() => setLyricsOpen((open) => !open)}><ListMusic /></Button></div><div className="flex items-center gap-1"><Button variant="ghost" size="icon" className="size-11 text-white/60" aria-label={muted ? t("playback.unmute") : t("playback.mute")} onClick={() => setMuted((value) => !value)}>{muted ? <VolumeX /> : <Volume2 />}</Button><input aria-label={t("playback.volume")} type="range" min={0} max={1} step={0.01} value={volume} onChange={(event) => setVolume(Number(event.target.value))} className="w-24 accent-emerald-300" /></div></div>
        </section>
      </main>
      {playerError ? <div className={`${inline ? "absolute" : "fixed"} inset-0 z-30 flex items-center justify-center bg-black/75 p-6`}><div className="flex max-w-sm flex-col items-center gap-3 rounded-lg border border-white/10 bg-[#121614] p-6 text-center"><TriangleAlert className="size-8 text-amber-300" /><h2 className="text-lg font-semibold">{t("playback.unavailable")}</h2><p className="text-sm text-white/55">{t("playback.unsupported")}</p><Button variant="secondary" onClick={() => { setPlayerError(false); resumedRef.current = false; autoplayAttemptedRef.current = false; setRetryNonce((value) => value + 1) }}><RotateCcw data-icon="inline-start" />{t("playback.retry")}</Button></div></div> : null}
    </div>
  )
}

function QueuePanel({ items, currentID, loading, onSelect, loadingLabel }: { items: MediaLibraryItem[]; currentID: number; loading: boolean; onSelect: (item: MediaLibraryItem) => void; loadingLabel: string }) {
  return <div className="max-h-[22rem] overflow-y-auto" data-audio-queue>{loading ? <div className="p-8 text-center text-sm text-white/45">{loadingLabel}</div> : items.map((candidate, index) => <button type="button" key={candidate.id} onClick={() => onSelect(candidate)} className={`flex min-h-14 w-full items-center gap-3 border-b border-white/5 px-2 text-left text-sm transition-colors hover:bg-white/[0.06] ${candidate.id === currentID ? "text-emerald-300" : "text-white/75"}`}><span className="w-7 text-right text-xs text-white/35">{index + 1}</span><span className="min-w-0 flex-1 truncate">{candidate.name}</span><span className="max-w-32 truncate text-xs text-white/35">{candidate.album_artist || candidate.album || ""}</span></button>)}</div>
}

interface LyricLine { time: number; text: string; translated?: string }
function LyricsPanel({ lyrics, activeIndex, setActiveIndex, lyricsRef, playerRef, t }: { lyrics: LyricLine[]; activeIndex: number; setActiveIndex: (index: number) => void; lyricsRef: React.RefObject<HTMLDivElement>; playerRef: React.RefObject<MediaPlayerInstance>; t: (key: "playback.lyricsUnavailable") => string }) {
  if (lyrics.length === 0) return <div className="flex min-h-[15rem] items-center justify-center text-sm text-white/45">{t("playback.lyricsUnavailable")}</div>
  return <div ref={lyricsRef} className="max-h-[22rem] overflow-y-auto py-4 text-center" data-audio-lyrics>{lyrics.map((line, index) => <button type="button" key={`${line.time}-${index}`} data-lyric-index={index} onClick={() => { setActiveIndex(index); if (playerRef.current) playerRef.current.currentTime = line.time }} className={`block min-h-11 w-full px-3 py-2 text-sm leading-6 transition-colors ${index === activeIndex ? "text-emerald-300" : "text-white/40 hover:text-white/75"}`}>{line.text}{line.translated ? <span className="mt-0.5 block text-xs opacity-75">{line.translated}</span> : null}</button>)}</div>
}

function mergeLyrics(original?: string, translated?: string): LyricLine[] {
  const originalLines = parseLyrics(original)
  const translatedLines = parseLyrics(translated)
  if (originalLines.length === 0) return translatedLines
  return originalLines.map((line) => ({ ...line, translated: translatedLines.find((candidate) => Math.abs(candidate.time - line.time) < 0.2)?.text }))
}

function parseLyrics(value?: string): LyricLine[] {
  if (!value?.trim()) return []
  const lines: LyricLine[] = []
  for (const raw of value.split(/\r?\n/)) {
    const match = raw.match(/^\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]\s*(.*)$/)
    if (!match) continue
    const fraction = match[3] ? Number(`0.${match[3].padEnd(3, "0")}`) : 0
    lines.push({ time: Number(match[1]) * 60 + Number(match[2]) + fraction, text: match[4].trim() })
  }
  const timed = lines.filter((line) => line.text).sort((left, right) => left.time - right.time)
  if (timed.length > 0) return timed
  return displayLyrics(value).split(/\r?\n/).map((text, index) => ({ time: index * Number.MAX_SAFE_INTEGER, text: text.trim() })).filter((line) => line.text)
}

function formatSeconds(value: number, locale: string) {
  if (!Number.isFinite(value) || value < 0) return locale.startsWith("zh") ? "0:00" : "0:00"
  const minutes = Math.floor(value / 60)
  const seconds = Math.floor(value % 60).toString().padStart(2, "0")
  return `${minutes}:${seconds}`
}

function PlaybackState({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-[#070908] px-6 text-white/65">
      {children}
    </main>
  )
}

function attemptAutoplay(
  playerRef: React.RefObject<MediaPlayerInstance>,
  attemptedRef: React.MutableRefObject<boolean>,
  onError: (value: boolean) => void,
) {
  if (attemptedRef.current || !playerRef.current) return
  attemptedRef.current = true
  void playerRef.current.play().catch((error: unknown) => {
    if (error instanceof DOMException && error.name === "NotAllowedError") return
    onError(true)
  })
}

function AudioArtwork({ item }: { item: MediaLibraryItem }) {
  return (
    <div className="mx-auto mb-5 flex aspect-square w-full max-w-72 items-center justify-center overflow-hidden rounded-lg bg-white/5 sm:max-w-80">
      {item.image_url ? (
        <img className="size-full object-cover" src={item.image_url} alt={item.name} />
      ) : (
        <Music2 className="size-16 text-white/25" strokeWidth={1.25} />
      )}
    </div>
  )
}

function AudioLyrics({ item }: { item: MediaLibraryItem }) {
	const { t } = useI18n()
	const original = displayLyrics(item.lyrics)
	const translated = displayLyrics(item.translated_lyrics)
	if (!original && !translated) return null

	return (
		<section className="grid gap-4 border-t border-white/10 px-1 py-6 lg:grid-cols-2" aria-label={t("playback.lyrics")}>
			{original ? <AudioLyricsBlock title={t("playback.originalLyrics")} value={original} /> : null}
			{translated ? <AudioLyricsBlock title={t("playback.translatedLyrics")} value={translated} /> : null}
		</section>
	)
}

function AudioLyricsBlock({ title, value }: { title: string; value: string }) {
	return (
		<div className="min-w-0 rounded-lg border border-white/10 bg-white/[0.03] p-4 sm:p-5">
			<h2 className="text-sm font-medium text-white/55">{title}</h2>
			<p className="mt-3 max-h-96 overflow-y-auto whitespace-pre-wrap text-sm leading-8 text-white/75">{value}</p>
		</div>
	)
}

function mediaContainer(item: MediaLibraryItem) {
  const declared = item.container?.trim().toLowerCase().replace(/^\./, "")
  if (declared) return declared
  const path = (item.media_path || item.path).split(/[?#]/, 1)[0]
  const filename = path.slice(path.lastIndexOf("/") + 1)
  return filename.includes(".")
    ? filename.slice(filename.lastIndexOf(".") + 1).toLowerCase()
    : ""
}

function isMediaFormatSupported(item: MediaLibraryItem) {
  const container = mediaContainer(item)
  return item.kind === "audio"
    ? Object.prototype.hasOwnProperty.call(audioMimeTypes, container)
    : Object.prototype.hasOwnProperty.call(videoMimeTypes, container)
}

function mediaMimeType(item: MediaLibraryItem): AudioMimeType | VideoMimeType {
  const container = mediaContainer(item)
  if (item.kind === "audio") {
    return audioMimeTypes[container] as AudioMimeType
  }
  return videoMimeTypes[container] as VideoMimeType
}

function mediaSubtitle(item: MediaLibraryItem) {
  const values = [
    item.production_year,
    item.album_artist,
    item.album,
    item.season_number !== undefined && item.episode_number !== undefined
      ? `S${item.season_number} E${item.episode_number}`
      : null,
  ]
  return values.filter(Boolean).join(" · ") || item.path
}

function formatRuntime(ticks: number, locale: string) {
  const totalMinutes = Math.max(1, Math.round(ticks / TICKS_PER_SECOND / 60))
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (locale.startsWith("zh")) {
    return hours === 0 ? `${minutes} 分钟` : `${hours} 小时 ${minutes} 分钟`
  }
  return hours === 0 ? `${minutes} min` : `${hours} hr ${minutes} min`
}
