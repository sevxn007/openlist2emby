import { FormEvent, useEffect, useState } from "react"
import { ArrowLeft, Check, TriangleAlert } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { OpenListObject, RenameObject } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import {
  BatchRenameConfig,
  BatchRenameMode,
  buildBatchRenamePlan,
  MediaEpisodeSelection,
  RenamePlanErrorCode,
} from "./batchRename"
import { MediaEpisodePickerSelection, MediaPicker } from "./MediaSearchPanel"
import {
  formatEpisodeCode,
  formatSeasonCode,
  getMediaDisplayTitle,
  isVideoFile,
} from "./mediaNaming"

type DialogStep = "configure" | "preview"

const modes: BatchRenameMode[] = ["regex", "sequence", "find", "media"]

export function BatchRenameDialog({
  allItems,
  error,
  isPending,
  open,
  selectedItems,
  onClose,
  onSubmit,
}: {
  allItems: OpenListObject[]
  error: Error | null
  isPending: boolean
  open: boolean
  selectedItems: OpenListObject[]
  onClose: () => void
  onSubmit: (renameObjects: RenameObject[]) => void
}) {
  const { t } = useI18n()
  const [step, setStep] = useState<DialogStep>("configure")
  const [mode, setMode] = useState<BatchRenameMode>("regex")
  const [pattern, setPattern] = useState("")
  const [regexReplacement, setRegexReplacement] = useState("")
  const [sequenceTemplate, setSequenceTemplate] = useState("")
  const [sequenceStart, setSequenceStart] = useState("1")
  const [sequencePadding, setSequencePadding] = useState("2")
  const [findText, setFindText] = useState("")
  const [findReplacement, setFindReplacement] = useState("")
  const [mediaSelection, setMediaSelection] = useState<MediaEpisodeSelection | null>(null)
  const [preview, setPreview] = useState<RenameObject[]>([])
  const [planError, setPlanError] = useState("")

  useEffect(() => {
    if (!open) {
      setStep("configure")
      setMode("regex")
      setPattern("")
      setRegexReplacement("")
      setSequenceTemplate("")
      setSequenceStart("1")
      setSequencePadding("2")
      setFindText("")
      setFindReplacement("")
      setMediaSelection(null)
      setPreview([])
      setPlanError("")
    }
  }, [open])

  const mediaEligible = selectedItems.every(isVideoFile)

  function selectMode(nextMode: BatchRenameMode) {
    setMode(nextMode)
    setPlanError("")
  }

  function getConfig(): BatchRenameConfig {
    if (mode === "regex") {
      return { mode, pattern, replacement: regexReplacement }
    }
    if (mode === "sequence") {
      return {
        mode,
        template: sequenceTemplate,
        start: sequenceStart,
        padding: sequencePadding.trim() ? Number(sequencePadding) : 0,
      }
    }
    if (mode === "find") {
      return { mode, search: findText, replacement: findReplacement }
    }
    return { mode, selection: mediaSelection }
  }

  function handlePreview(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault()
    const result = buildBatchRenamePlan(getConfig(), selectedItems, allItems)
    if (!result.ok) {
      setPlanError(getRenamePlanError(result.code, result.name, t))
      return
    }
    setPlanError("")
    setPreview(result.renameObjects)
    setStep("preview")
  }

  function handleMediaSelection(selection: MediaEpisodePickerSelection | null) {
    if (!selection) {
      setMediaSelection(null)
      return
    }
    setMediaSelection({
      item: selection.item,
      season: selection.season,
      episodes: selection.episodes,
      startEpisodeNumber: selection.episode.episode_number,
    })
    setPlanError("")
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && !isPending && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            {step === "preview" ? t("files.renamePreview") : t("files.batchRename")}
          </DialogTitle>
          <DialogDescription>
            {t("files.selectedCount", { count: selectedItems.length })}
          </DialogDescription>
        </DialogHeader>

        {step === "configure" ? (
          <form
            id="batch-rename-form"
            className="min-h-0 overflow-y-auto pr-1"
            onSubmit={handlePreview}
          >
            <div
              role="tablist"
              aria-label={t("files.renameMode")}
              className="mb-5 grid grid-cols-2 gap-1 rounded-lg bg-muted p-1 md:grid-cols-4"
            >
              {modes.map((item) => (
                <Button
                  key={item}
                  type="button"
                  role="tab"
                  aria-selected={mode === item}
                  variant={mode === item ? "secondary" : "ghost"}
                  className="min-h-11 whitespace-normal"
                  onClick={() => selectMode(item)}
                >
                  {t(`files.renameMode.${item}`)}
                </Button>
              ))}
            </div>

            {mode === "regex" ? (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="batch-regex-pattern">{t("files.regexPattern")}</FieldLabel>
                  <Input
                    id="batch-regex-pattern"
                    value={pattern}
                    onChange={(event) => setPattern(event.target.value)}
                    placeholder={t("files.regexPatternPlaceholder")}
                    autoFocus
                    required
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="batch-regex-replacement">{t("files.replacement")}</FieldLabel>
                  <Input
                    id="batch-regex-replacement"
                    value={regexReplacement}
                    onChange={(event) => setRegexReplacement(event.target.value)}
                    placeholder={t("files.replacementPlaceholder")}
                  />
                </Field>
              </FieldGroup>
            ) : null}

            {mode === "sequence" ? (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="batch-sequence-template">
                    {t("files.sequenceTemplate")}
                  </FieldLabel>
                  <Input
                    id="batch-sequence-template"
                    value={sequenceTemplate}
                    onChange={(event) => setSequenceTemplate(event.target.value)}
                    placeholder={t("files.sequenceTemplatePlaceholder")}
                    autoFocus
                    required
                  />
                </Field>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="batch-sequence-start">
                      {t("files.startNumber")}
                    </FieldLabel>
                    <Input
                      id="batch-sequence-start"
                      type="number"
                      min="0"
                      step="1"
                      value={sequenceStart}
                      onChange={(event) => setSequenceStart(event.target.value)}
                      required
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="batch-sequence-padding">
                      {t("files.zeroPadding")}
                    </FieldLabel>
                    <Input
                      id="batch-sequence-padding"
                      type="number"
                      min="0"
                      step="1"
                      value={sequencePadding}
                      onChange={(event) => setSequencePadding(event.target.value)}
                    />
                  </Field>
                </div>
              </FieldGroup>
            ) : null}

            {mode === "find" ? (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="batch-find-text">{t("files.findText")}</FieldLabel>
                  <Input
                    id="batch-find-text"
                    value={findText}
                    onChange={(event) => setFindText(event.target.value)}
                    autoFocus
                    required
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="batch-find-replacement">
                    {t("files.replacement")}
                  </FieldLabel>
                  <Input
                    id="batch-find-replacement"
                    value={findReplacement}
                    onChange={(event) => setFindReplacement(event.target.value)}
                  />
                </Field>
              </FieldGroup>
            ) : null}

            {mode === "media" ? (
              <div className="flex flex-col gap-3">
                {!mediaEligible ? (
                  <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
                    <TriangleAlert className="size-4 shrink-0" />
                    {t("files.renameError.mediaVideoOnly")}
                  </div>
                ) : (
                  <>
                    {mediaSelection ? (
                      <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-card px-3 py-2 text-sm">
                        <Check className="size-4 text-primary" />
                        <span className="min-w-0 truncate font-medium">
                          {getMediaDisplayTitle(mediaSelection.item)}
                        </span>
                        <span className="text-muted-foreground">
                          {formatSeasonCode(mediaSelection.season.season_number)}
                          {formatEpisodeCode(mediaSelection.startEpisodeNumber)}
                        </span>
                      </div>
                    ) : null}
                    <MediaPicker
                      tvOnly
                      showSearchHistory
                      onEpisodeSelect={handleMediaSelection}
                    />
                  </>
                )}
              </div>
            ) : null}

            {planError ? (
              <p className="mt-4 text-sm text-destructive" role="alert">
                {planError}
              </p>
            ) : null}
          </form>
        ) : (
          <RenamePreview renameObjects={preview} />
        )}

        {error ? <p className="text-sm text-destructive">{error.message}</p> : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={isPending}>
            {t("settings.cancel")}
          </Button>
          {step === "preview" ? (
            <>
              <Button
                type="button"
                variant="outline"
                onClick={() => setStep("configure")}
                disabled={isPending}
              >
                <ArrowLeft data-icon="inline-start" />
                {t("files.back")}
              </Button>
              <Button
                type="button"
                onClick={() => onSubmit(preview)}
                disabled={isPending || preview.length === 0}
              >
                {isPending ? <Spinner data-icon="inline-start" /> : null}
                {t("files.confirmRename")}
              </Button>
            </>
          ) : (
            <Button
              type="submit"
              form="batch-rename-form"
              disabled={mode === "media" && !mediaEligible}
            >
              {t("files.preview")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RenamePreview({ renameObjects }: { renameObjects: RenameObject[] }) {
  const { t } = useI18n()

  return (
    <div className="min-h-0 overflow-y-auto rounded-lg border">
      <div className="sticky top-0 z-10 hidden grid-cols-2 border-b bg-muted/95 text-xs font-medium text-muted-foreground backdrop-blur sm:grid">
        <div className="px-4 py-3">{t("files.oldName")}</div>
        <div className="px-4 py-3">{t("files.newName")}</div>
      </div>
      <div className="divide-y">
        {renameObjects.map((item) => (
          <div
            key={item.src_name}
            className="grid grid-cols-1 gap-2 px-4 py-3 text-sm sm:grid-cols-2 sm:gap-4"
          >
            <div className="min-w-0">
              <span className="mb-1 block text-xs text-muted-foreground sm:hidden">
                {t("files.oldName")}
              </span>
              <span className="break-words">{item.src_name}</span>
            </div>
            <div className="min-w-0">
              <span className="mb-1 block text-xs text-muted-foreground sm:hidden">
                {t("files.newName")}
              </span>
              <span className="break-words font-medium text-primary">{item.new_name}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function getRenamePlanError(
  code: RenamePlanErrorCode,
  name: string | undefined,
  t: ReturnType<typeof useI18n>["t"],
) {
  const values = name ? { name } : undefined
  switch (code) {
    case "noSelection":
      return t("files.renameError.noSelection")
    case "invalidRegex":
      return t("files.renameError.invalidRegex")
    case "invalidSequence":
      return t("files.renameError.invalidSequence")
    case "invalidName":
      return t("files.renameError.invalidName", values)
    case "noMatches":
      return t("files.renameError.noMatches")
    case "duplicateTarget":
      return t("files.renameError.duplicateTarget", values)
    case "nameConflict":
      return t("files.renameError.nameConflict", values)
    case "mediaVideoOnly":
      return t("files.renameError.mediaVideoOnly")
    case "mediaSelectionRequired":
      return t("files.renameError.mediaSelectionRequired")
    case "insufficientEpisodes":
      return t("files.renameError.insufficientEpisodes")
  }
}
