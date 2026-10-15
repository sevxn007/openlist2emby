import { useEffect, useMemo, useRef, useState } from "react"
import { ArrowRight, Copy, FolderCheck, LibraryBig, PencilLine, Trash2, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"
import { Spinner } from "@/components/ui/spinner"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  joinOpenListPath,
  listFiles,
  normalizeOpenListPath,
  OpenListObject,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { CreateFolderDialog } from "./CreateFolderButton"
import { DirectoryTree } from "./DirectoryTree"

export type TransferKind = "move" | "copy"

type ToolbarActionTone = "rename" | "copy" | "move" | "delete" | "import"

const toolbarActionToneClassNames: Record<ToolbarActionTone, string> = {
  import: "text-primary hover:bg-primary/10 hover:text-primary",
  rename: "text-file-text hover:bg-file-text/10 hover:text-file-text",
  copy: "text-file-document hover:bg-file-document/10 hover:text-file-document",
  move: "text-file-audio hover:bg-file-audio/10 hover:text-file-audio",
  delete: "text-destructive hover:bg-destructive/10 hover:text-destructive",
}

export function BulkActionBar({
  count,
  onClear,
  onCopy,
  onDelete,
  onMove,
  onRename,
  onImport,
}: {
  count: number
  onClear: () => void
  onCopy: () => void
  onDelete: () => void
  onMove: () => void
  onRename: () => void
  onImport?: () => void
}) {
  const { t } = useI18n()

  return (
    <div className="pointer-events-none fixed right-4 bottom-[max(1rem,env(safe-area-inset-bottom))] left-4 z-40 flex justify-center md:right-0 md:left-(--app-sidebar-offset)">
      <div
        role="toolbar"
        aria-label={t("files.bulkActions")}
        className="pointer-events-auto flex max-w-full flex-wrap justify-center animate-in items-center gap-1 rounded-xl border bg-popover/95 p-2 text-popover-foreground shadow-2xl backdrop-blur-md duration-150 fade-in slide-in-from-bottom-2"
      >
        <span className="flex h-9 min-w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 px-2 text-sm font-semibold text-primary">
          {count}
        </span>
        <Separator orientation="vertical" className="mx-1 h-7" />
        {onImport ? <ToolbarButton icon={LibraryBig} label={t("files.importMedia")} onClick={onImport} tone="import" /> : null}
        <ToolbarButton
          icon={PencilLine}
          label={t("files.batchRename")}
          onClick={onRename}
          tone="rename"
        />
        <ToolbarButton icon={Copy} label={t("files.copy")} onClick={onCopy} tone="copy" />
        <ToolbarButton icon={ArrowRight} label={t("files.move")} onClick={onMove} tone="move" />
        <ToolbarButton
          icon={Trash2}
          label={t("files.delete")}
          onClick={onDelete}
          tone="delete"
        />
        <Separator orientation="vertical" className="mx-1 h-7" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-lg"
              aria-label={t("files.clearSelection")}
              onClick={onClear}
            >
              <X />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("files.clearSelection")}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}

function ToolbarButton({
  icon: Icon,
  label,
  onClick,
  tone,
}: {
  icon: typeof ArrowRight
  label: string
  onClick: () => void
  tone: ToolbarActionTone
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-lg"
          className={cn(toolbarActionToneClassNames[tone])}
          aria-label={label}
          onClick={onClick}
        >
          <Icon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

export function TransferDialog({
  count,
  currentPath,
  error,
  isPending,
  kind,
  listFilesFn = listFiles,
  objects,
  open,
  onClose,
  onSubmit,
}: {
  count: number
  currentPath: string
  error: Error | null
  isPending: boolean
  kind: TransferKind | null
  listFilesFn?: typeof listFiles
  objects: OpenListObject[]
  open: boolean
  onClose: () => void
  onSubmit: (destination: string) => void
}) {
  const { t } = useI18n()
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [creationParent, setCreationParent] = useState<string | null>(null)
  const creationTrigger = useRef<HTMLButtonElement | null>(null)
  const [revealPath, setRevealPath] = useState<string | null>(null)
  const [destination, setDestination] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setDestination(null)
      setCreatingFolder(false)
      setCreationParent(null)
      creationTrigger.current = null
      setRevealPath(null)
    }
  }, [open])

  const normalizedCurrentPath = normalizeOpenListPath(currentPath)
  const blockedDirectoryRoots = useMemo(
    () =>
      objects
        .filter((item) => item.is_dir)
        .map((item) => joinOpenListPath(normalizedCurrentPath, item.name)),
    [normalizedCurrentPath, objects],
  )

  const title = kind === "move" ? t("files.moveItems") : t("files.copyItems")

  function isSourceDirectory(path: string) {
    const normalizedPath = normalizeOpenListPath(path)
    return blockedDirectoryRoots.some(
      (sourcePath) =>
        normalizedPath === sourcePath || normalizedPath.startsWith(`${sourcePath}/`),
    )
  }

  const canTransfer = destination !== null &&
    destination !== normalizedCurrentPath && !isSourceDirectory(destination)

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && !isPending && !creatingFolder && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{t("files.selectedCount", { count })}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3 pr-1">
          <fieldset disabled={isPending || creatingFolder} className="min-h-0">
            <DirectoryTree
              revealPath={revealPath}
              isDisabled={isSourceDirectory}
              listFilesFn={listFilesFn}
              selectedPath={destination}
              onSelect={setDestination}
              onCreateFolder={isPending ? undefined : (path, trigger) => {
                setCreationParent(path)
                creationTrigger.current = trigger
                setCreatingFolder(true)
              }}
            />
          </fieldset>
          <CreateFolderDialog
            open={open && creatingFolder}
            parentPath={creationParent}
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              creationTrigger.current?.focus()
            }}
            listFilesFn={listFilesFn}
            onOpenChange={setCreatingFolder}
            onCreated={(path) => {
              setRevealPath(path)
              if (!isSourceDirectory(path)) setDestination(path)
            }}
          />
          {destination ? (
            <div
              className="flex min-h-11 min-w-0 items-center gap-2 rounded-lg border bg-primary/5 px-3 text-sm text-primary"
              aria-label={t("files.selectedDestination", { path: destination })}
            >
              <FolderCheck className="size-4 shrink-0" />
              <span className="truncate font-medium" title={destination}>{destination}</span>
            </div>
          ) : null}
          {error ? <p className="text-sm text-destructive">{error.message}</p> : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={isPending || creatingFolder}>
            {t("settings.cancel")}
          </Button>
          <Button
            type="button"
            disabled={isPending || creatingFolder || !canTransfer}
            onClick={() => canTransfer && destination && onSubmit(destination)}
          >
            {isPending ? <Spinner data-icon="inline-start" /> : null}
            {title}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DeleteDialog({
  count,
  error,
  isPending,
  open,
  onClose,
  onConfirm,
}: {
  count: number
  error: Error | null
  isPending: boolean
  open: boolean
  onClose: () => void
  onConfirm: () => void
}) {
  const { t } = useI18n()

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && !isPending && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("files.deleteItems")}</DialogTitle>
          <DialogDescription>
            {t("files.deleteConfirmation", { count })}
          </DialogDescription>
        </DialogHeader>
        {error ? <p className="text-sm text-destructive">{error.message}</p> : null}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={isPending}>
            {t("settings.cancel")}
          </Button>
          <Button type="button" variant="destructive" onClick={onConfirm} disabled={isPending}>
            {isPending ? <Spinner data-icon="inline-start" /> : <Trash2 data-icon="inline-start" />}
            {t("files.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
