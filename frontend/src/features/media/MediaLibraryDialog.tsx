import { FormEvent, useEffect, useRef, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarClock, FolderOpen, Save, X } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
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
import { Switch } from "@/components/ui/switch"
import { DirectoryTree } from "@/features/files/DirectoryTree"
import {
  createMediaLibrary,
  getMediaLibraryScanSchedule,
  ManagerApiError,
  MediaLibrary,
  MediaLibraryContentType,
  updateMediaLibrary,
  updateMediaLibraryScanSchedule,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

interface MediaLibraryDialogProps {
  library: MediaLibrary | null
  open: boolean
  onClose: () => void
  onSaved: (library: MediaLibrary) => void
}

export function MediaLibraryDialog({
  library,
  open,
  onClose,
  onSaved,
}: MediaLibraryDialogProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const [contentType, setContentType] =
    useState<MediaLibraryContentType>("movies")
  const [rootPaths, setRootPaths] = useState<string[]>([])
  const [videoScreenshotEnabled, setVideoScreenshotEnabled] = useState(false)
  const [videoScreenshotPercent, setVideoScreenshotPercent] = useState("10")
  const [playbackCompletionPercent, setPlaybackCompletionPercent] = useState("90")
  const [hidePeopleWithoutImages, setHidePeopleWithoutImages] = useState(false)
  const [scanScheduleEnabled, setScanScheduleEnabled] = useState(true)
  const [scanCronExpression, setScanCronExpression] = useState("0 * * * *")
  const [scanTimezone, setScanTimezone] = useState("UTC")
  const [folderPickerOpen, setFolderPickerOpen] = useState(false)
  const [draftRootPaths, setDraftRootPaths] = useState<string[]>([])
  const [error, setError] = useState("")
  const createdLibraryRef = useRef<MediaLibrary | null>(null)

  const scanScheduleQuery = useQuery({
    queryKey: ["media-library-scan-schedule", library?.id],
    queryFn: () => getMediaLibraryScanSchedule(library?.id ?? 0),
    enabled: open && library !== null,
  })

  useEffect(() => {
    if (!open) return
    setName(library?.name ?? "")
    setContentType(library?.content_type ?? "movies")
    setRootPaths(
      library?.root_paths?.length
        ? library.root_paths
        : library?.root_path
          ? [library.root_path]
          : [],
    )
    setVideoScreenshotEnabled(library?.video_screenshot_enabled ?? false)
    setVideoScreenshotPercent(String(library?.video_screenshot_percent ?? 10))
    setPlaybackCompletionPercent(String(library?.playback_completion_percent ?? 90))
    setHidePeopleWithoutImages(library?.hide_people_without_images ?? false)
    setScanScheduleEnabled(true)
    setScanCronExpression("0 * * * *")
    setScanTimezone("UTC")
    setError("")
    setFolderPickerOpen(false)
    createdLibraryRef.current = null
  }, [library, open])

  useEffect(() => {
    if (!open || !library || !scanScheduleQuery.data) return
    setScanScheduleEnabled(scanScheduleQuery.data.enabled)
    setScanCronExpression(scanScheduleQuery.data.cron_expression)
    setScanTimezone(scanScheduleQuery.data.timezone)
  }, [library, open, scanScheduleQuery.data])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const payload = {
        name: name.trim(),
        content_type: contentType,
        root_paths: rootPaths,
        video_screenshot_enabled:
          contentType !== "music" && videoScreenshotEnabled,
        video_screenshot_percent:
          contentType === "music" ? 10 : Number(videoScreenshotPercent),
        playback_completion_percent: Number(playbackCompletionPercent),
        hide_people_without_images: hidePeopleWithoutImages,
      }
      let saved: MediaLibrary
      if (library) {
        saved = await updateMediaLibrary(library.id, payload)
      } else if (createdLibraryRef.current) {
        saved = await updateMediaLibrary(createdLibraryRef.current.id, payload)
      } else {
        saved = await createMediaLibrary(payload)
        createdLibraryRef.current = saved
      }
      await updateMediaLibraryScanSchedule(saved.id, {
        cron_expression: scanCronExpression.trim(),
        timezone: scanTimezone.trim(),
        enabled: scanScheduleEnabled,
      })
      return saved
    },
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
      void queryClient.invalidateQueries({ queryKey: ["media-library-items"] })
      void queryClient.invalidateQueries({
        queryKey: ["media-library-scan-schedule", saved.id],
      })
      void queryClient.invalidateQueries({ queryKey: ["tasks", "schedules"] })
      toast.success(t("mediaLibrary.saved"))
      createdLibraryRef.current = null
      onSaved(saved)
      onClose()
    },
    onError: (err) => {
      if (createdLibraryRef.current) {
        void queryClient.invalidateQueries({ queryKey: ["media-libraries"] })
      }
      setError(
        err instanceof ManagerApiError
          ? err.message
          : t("mediaLibrary.saveFailed"),
      )
    },
  })

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const screenshotPercent = Number(videoScreenshotPercent)
    const screenshotPercentInvalid =
      contentType !== "music" &&
      (!Number.isInteger(screenshotPercent) ||
        screenshotPercent < 1 ||
        screenshotPercent > 99)
    const completionPercent = Number(playbackCompletionPercent)
    const completionPercentInvalid =
      !Number.isInteger(completionPercent) ||
      completionPercent < 1 ||
      completionPercent > 100
    if (screenshotPercentInvalid) {
      setError(t("mediaLibrary.videoScreenshotPercentInvalid"))
      return
    }
    if (completionPercentInvalid) {
      setError(t("mediaLibrary.playbackCompletionPercentInvalid"))
      return
    }
    if (
      !name.trim() ||
      rootPaths.length === 0 ||
      !scanCronExpression.trim() ||
      !scanTimezone.trim()
    ) {
      setError(
        !scanCronExpression.trim() || !scanTimezone.trim()
          ? t("mediaLibrary.scanScheduleRequired")
          : t("mediaLibrary.requiredFields"),
      )
      return
    }
    saveMutation.mutate()
  }

  const videoScreenshotPercentInvalid =
    contentType !== "music" &&
    (!Number.isInteger(Number(videoScreenshotPercent)) ||
      Number(videoScreenshotPercent) < 1 ||
      Number(videoScreenshotPercent) > 99)
  const playbackCompletionPercentInvalid =
    !Number.isInteger(Number(playbackCompletionPercent)) ||
    Number(playbackCompletionPercent) < 1 ||
      Number(playbackCompletionPercent) > 100
  const scheduleUnavailable = library !== null && scanScheduleQuery.isError

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-xl">
        <form onSubmit={handleSubmit} className="contents">
          <DialogHeader>
            <DialogTitle>
              {library
                ? t("mediaLibrary.editLibrary")
                : t("mediaLibrary.addLibrary")}
            </DialogTitle>
          </DialogHeader>
          <DialogBody className="pr-1">
            <FieldGroup>
              <Field data-invalid={Boolean(error)}>
              <FieldLabel htmlFor="media-library-name">
                {t("mediaLibrary.name")}
              </FieldLabel>
              <Input
                id="media-library-name"
                value={name}
                maxLength={100}
                onChange={(event) => {
                  setName(event.target.value)
                  setError("")
                }}
                required
              />
              </Field>
              <Field>
              <FieldLabel htmlFor="media-library-type">
                {t("mediaLibrary.type")}
              </FieldLabel>
              <Select
                value={contentType}
                onValueChange={(value) => {
                  const nextType = value as MediaLibraryContentType
                  setContentType(nextType)
                  if (nextType === "music") setVideoScreenshotEnabled(false)
                  setError("")
                }}
              >
                <SelectTrigger id="media-library-type" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="movies">
                      {t("mediaLibrary.type.movies")}
                    </SelectItem>
                    <SelectItem value="tvshows">
                      {t("mediaLibrary.type.tvshows")}
                    </SelectItem>
                    <SelectItem value="music">
                      {t("mediaLibrary.type.music")}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              </Field>
              <Field data-invalid={playbackCompletionPercentInvalid}>
                <FieldLabel htmlFor="media-library-playback-completion-percent">
                  {t("mediaLibrary.playbackCompletionPercent")}
                </FieldLabel>
                <div className="flex items-center gap-2">
                  <Input
                    id="media-library-playback-completion-percent"
                    className="max-w-28"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    max={100}
                    value={playbackCompletionPercent}
                    aria-invalid={playbackCompletionPercentInvalid}
                    onChange={(event) => {
                      setPlaybackCompletionPercent(event.target.value)
                      setError("")
                    }}
                  />
                  <span className="text-sm text-muted-foreground">%</span>
                </div>
              </Field>
              {contentType !== "music" ? (
              <>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="media-library-hide-people-without-images">
                    {t("mediaLibrary.hidePeopleWithoutImages")}
                  </FieldLabel>
                  <Switch
                    id="media-library-hide-people-without-images"
                    checked={hidePeopleWithoutImages}
                    onCheckedChange={(checked) => {
                      setHidePeopleWithoutImages(checked)
                      setError("")
                    }}
                  />
                </Field>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="media-library-video-screenshot">
                    {t("mediaLibrary.videoScreenshot")}
                  </FieldLabel>
                  <Switch
                    id="media-library-video-screenshot"
                    checked={videoScreenshotEnabled}
                    onCheckedChange={(checked) => {
                      setVideoScreenshotEnabled(checked)
                      setError("")
                    }}
                  />
                </Field>
                <Field data-disabled={!videoScreenshotEnabled} data-invalid={videoScreenshotPercentInvalid}>
                  <FieldLabel htmlFor="media-library-video-screenshot-percent">
                    {t("mediaLibrary.videoScreenshotPercent")}
                  </FieldLabel>
                  <div className="flex items-center gap-2">
                    <Input
                      id="media-library-video-screenshot-percent"
                      className="max-w-28"
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={99}
                      value={videoScreenshotPercent}
                      disabled={!videoScreenshotEnabled}
                      aria-invalid={videoScreenshotPercentInvalid}
                      onChange={(event) => {
                        setVideoScreenshotPercent(event.target.value)
                        setError("")
                      }}
                    />
                    <span className="text-sm text-muted-foreground">%</span>
                  </div>
                </Field>
              </>
              ) : null}
              <Field orientation="horizontal">
                <FieldLabel htmlFor="media-library-scan-schedule">
                  <span className="flex items-center gap-2">
                    <CalendarClock className="size-4" />
                    {t("mediaLibrary.scanSchedule")}
                  </span>
                </FieldLabel>
                <Switch
                  id="media-library-scan-schedule"
                  checked={scanScheduleEnabled}
                  disabled={scanScheduleQuery.isLoading}
                  onCheckedChange={(checked) => {
                    setScanScheduleEnabled(checked)
                    setError("")
                  }}
                />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field data-invalid={!scanCronExpression.trim()}>
                  <FieldLabel htmlFor="media-library-scan-cron">
                    {t("tasks.schedule")}
                  </FieldLabel>
                  <Input
                    id="media-library-scan-cron"
                    value={scanCronExpression}
                    disabled={scanScheduleQuery.isLoading}
                    placeholder="0 * * * *"
                    onChange={(event) => {
                      setScanCronExpression(event.target.value)
                      setError("")
                    }}
                  />
                </Field>
                <Field data-invalid={!scanTimezone.trim()}>
                  <FieldLabel htmlFor="media-library-scan-timezone">
                    {t("tasks.timezone")}
                  </FieldLabel>
                  <Input
                    id="media-library-scan-timezone"
                    value={scanTimezone}
                    disabled={scanScheduleQuery.isLoading}
                    placeholder="UTC"
                    onChange={(event) => {
                      setScanTimezone(event.target.value)
                      setError("")
                    }}
                  />
                </Field>
              </div>
              {scheduleUnavailable ? (
                <FieldError>{t("mediaLibrary.scanScheduleLoadFailed")}</FieldError>
              ) : null}
              <Field data-invalid={Boolean(error)}>
              <FieldLabel>{t("mediaLibrary.folder")}</FieldLabel>
              <Button
                type="button"
                variant="outline"
                className="w-full justify-start gap-2"
                onClick={() => {
                  setDraftRootPaths(rootPaths)
                  setFolderPickerOpen(true)
                }}
              >
                <FolderOpen className="size-4 shrink-0" />
                <span className="min-w-0 truncate text-left">
                  {rootPaths.length > 0
                    ? rootPaths.join(", ")
                    : t("mediaLibrary.selectFolders")}
                </span>
              </Button>
              {error ? <FieldError>{error}</FieldError> : null}
              </Field>
            </FieldGroup>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t("settings.cancel")}
            </Button>
            <Button
              type="submit"
              disabled={saveMutation.isPending || scanScheduleQuery.isLoading || scheduleUnavailable}
            >
              {saveMutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Save data-icon="inline-start" />
              )}
              {t("mediaLibrary.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
      <Dialog open={folderPickerOpen} onOpenChange={setFolderPickerOpen}>
        <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("mediaLibrary.selectFolders")}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pr-1">
            <DirectoryTree
              ariaLabel={t("mediaLibrary.folder")}
              isDisabled={(path) =>
                draftRootPaths.some(
                  (rootPath) => rootPath !== path && pathsOverlap(rootPath, path),
                )
              }
              selectedPath={null}
              selectedPaths={draftRootPaths}
              onSelect={(path) => {
                setDraftRootPaths((paths) =>
                  paths.includes(path)
                    ? paths.filter((rootPath) => rootPath !== path)
                    : [...paths, path],
                )
              }}
            />
            {draftRootPaths.length > 0 ? (
              <ul className="mt-3 max-h-36 divide-y overflow-y-auto rounded-lg border bg-muted/20" aria-label={t("mediaLibrary.folder")}>
                {draftRootPaths.map((rootPath) => (
                  <li className="flex min-w-0 items-center gap-2 px-3 py-1.5" key={rootPath}>
                    <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={rootPath}>
                      {rootPath}
                    </span>
                    <Button type="button" variant="ghost" size="icon-sm" aria-label={t("mediaLibrary.removeFolder", { path: rootPath })} onClick={() => setDraftRootPaths((paths) => paths.filter((path) => path !== rootPath))}>
                      <X />
                    </Button>
                  </li>
                ))}
              </ul>
            ) : null}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setFolderPickerOpen(false)}>
              {t("settings.cancel")}
            </Button>
            <Button type="button" onClick={() => { setRootPaths(draftRootPaths); setFolderPickerOpen(false); setError("") }}>
              {t("mediaLibrary.confirmFolders")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Dialog>
  )
}

function pathsOverlap(left: string, right: string) {
  return (
    left === "/" ||
    right === "/" ||
    left === right ||
    left.startsWith(`${right}/`) ||
    right.startsWith(`${left}/`)
  )
}
