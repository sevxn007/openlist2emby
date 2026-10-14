import {
  FormEvent,
  Fragment,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  ArrowUpDown,
  Copy,
  Download,
  File,
  FileArchive,
  FileAudio,
  FileCode2,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileType2,
  FileVideo,
  FolderOpen,
  Pencil,
  RefreshCw,
  Trash2,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import {
  batchRenameFiles,
  buildDownloadUrl,
  copyFiles,
  joinOpenListPath,
  listFiles,
  ManagerApiError,
  moveFiles,
  normalizeOpenListPath,
  OpenListObject,
  removeFiles,
  RenameObject,
  renameFile,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { formatBytes, formatDate } from "@/shared/lib/format"
import { CreateFolderButton } from "./CreateFolderButton"
import { MediaSearchPanel } from "./MediaSearchPanel"
import { supportsMediaSearch } from "./mediaNaming"
import { BatchRenameDialog } from "./BatchRenameDialog"
import {
  BulkActionBar,
  DeleteDialog,
  TransferDialog,
  TransferKind,
} from "./BulkActions"
import {
  getFilenameExtension,
  supportsMusicMetadata,
} from "./musicMetadata"

type ListFilesFn = typeof listFiles
type OperationKind = "rename" | "move" | "copy"
type FileSortDirection = "asc" | "desc"
type FileSortKey = "name" | "size" | "modified"
type FileVisualKind =
  | "folder"
  | "image"
  | "video"
  | "audio"
  | "pdf"
  | "document"
  | "sheet"
  | "code"
  | "archive"
  | "text"
  | "generic"

interface PendingRenameOperation {
  kind: "rename"
  obj: OpenListObject
}

interface PendingTransferOperation {
  kind: TransferKind
  objects: OpenListObject[]
  bulk: boolean
}

type PendingOperation = PendingRenameOperation | PendingTransferOperation

interface PendingDeleteOperation {
  objects: OpenListObject[]
  bulk: boolean
}

interface FileBrowserProps {
  openListBaseURL?: string
  listFilesFn?: ListFilesFn
}

interface FileRowData extends OpenListObject {
  key: string
}

interface FileSort {
  direction: FileSortDirection
  key: FileSortKey
}

interface SelectionDragState {
  anchorIndex: number
  currentIndex: number
  initialSelection: Set<string>
  keyIndexes: Map<string, number>
  moved: boolean
  orderedKeys: string[]
  pointerId: number
  startX: number
  startY: number
  targetChecked: boolean
}

const selectionDragThreshold = 6

const defaultFileSort: FileSort = { key: "name", direction: "asc" }

const defaultSortDirections: Record<FileSortKey, FileSortDirection> = {
  name: "asc",
  size: "asc",
  modified: "desc",
}

const fileSortOptions = [
  { key: "name", direction: "asc", labelKey: "files.sortOption.nameAsc", value: "name:asc" },
  { key: "name", direction: "desc", labelKey: "files.sortOption.nameDesc", value: "name:desc" },
  { key: "size", direction: "asc", labelKey: "files.sortOption.sizeAsc", value: "size:asc" },
  { key: "size", direction: "desc", labelKey: "files.sortOption.sizeDesc", value: "size:desc" },
  {
    key: "modified",
    direction: "desc",
    labelKey: "files.sortOption.modifiedDesc",
    value: "modified:desc",
  },
  {
    key: "modified",
    direction: "asc",
    labelKey: "files.sortOption.modifiedAsc",
    value: "modified:asc",
  },
] as const

const fileToneClassNames: Record<FileVisualKind, string> = {
  folder: "bg-file-folder/10 text-file-folder ring-file-folder/15",
  image: "bg-file-image/10 text-file-image ring-file-image/15",
  video: "bg-file-video/10 text-file-video ring-file-video/15",
  audio: "bg-file-audio/10 text-file-audio ring-file-audio/15",
  pdf: "bg-file-pdf/10 text-file-pdf ring-file-pdf/15",
  document: "bg-file-document/10 text-file-document ring-file-document/15",
  sheet: "bg-file-sheet/10 text-file-sheet ring-file-sheet/15",
  code: "bg-file-code/10 text-file-code ring-file-code/15",
  archive: "bg-file-archive/10 text-file-archive ring-file-archive/15",
  text: "bg-file-text/10 text-file-text ring-file-text/15",
  generic: "bg-muted text-muted-foreground ring-border",
}

function getPathFromUrl() {
  const params = new URLSearchParams(window.location.search)
  return normalizeOpenListPath(params.get("path") ?? "/")
}

function setPathInUrl(path: string) {
  const normalized = normalizeOpenListPath(path)
  const url = new URL(window.location.href)
  url.searchParams.delete("section")
  if (normalized === "/") {
    url.searchParams.delete("path")
  } else {
    url.searchParams.set("path", normalized)
  }
  window.history.pushState({}, "", url)
}

function compareNames(left: string, right: string, locale: string) {
  return left.localeCompare(right, locale, {
    numeric: true,
    sensitivity: "base",
  })
}

function compareModified(left: string, right: string, locale: string) {
  const leftTime = Date.parse(left)
  const rightTime = Date.parse(right)
  if (Number.isNaN(leftTime) || Number.isNaN(rightTime)) {
    return compareNames(left, right, locale)
  }
  return leftTime - rightTime
}

function sortObjects(objects: OpenListObject[], sort: FileSort, locale: string) {
  return [...objects].sort((a, b) => {
    if (a.is_dir !== b.is_dir) {
      return a.is_dir ? -1 : 1
    }

    let comparison: number
    if (sort.key === "size") {
      comparison = a.size - b.size
    } else if (sort.key === "modified") {
      comparison = compareModified(a.modified, b.modified, locale)
    } else {
      comparison = compareNames(a.name, b.name, locale)
    }
    if (comparison === 0 && sort.key !== "name") {
      comparison = compareNames(a.name, b.name, locale)
    }
    return sort.direction === "asc" ? comparison : -comparison
  })
}

function getFileVisual(obj: OpenListObject): {
  Icon: LucideIcon
  badge?: string
  kind: FileVisualKind
} {
  if (obj.is_dir) {
    return { Icon: FolderOpen, kind: "folder" }
  }

  const ext = getFilenameExtension(obj.name)
  const badge = ext ? ext.slice(0, 4).toUpperCase() : undefined

  if (["jpg", "jpeg", "png", "gif", "webp", "avif", "svg", "heic"].includes(ext)) {
    return { Icon: FileImage, badge, kind: "image" }
  }
  if (supportsMediaSearch(obj)) {
    return { Icon: FileVideo, badge, kind: "video" }
  }
  if (supportsMusicMetadata(obj)) {
    return { Icon: FileAudio, badge, kind: "audio" }
  }
  if (ext === "pdf") {
    return { Icon: FileType2, badge, kind: "pdf" }
  }
  if (["doc", "docx", "rtf", "odt"].includes(ext)) {
    return { Icon: FileText, badge, kind: "document" }
  }
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) {
    return { Icon: FileSpreadsheet, badge, kind: "sheet" }
  }
  if (["js", "jsx", "ts", "tsx", "go", "rs", "py", "java", "json", "yaml", "yml", "css", "html", "sql", "sh"].includes(ext)) {
    return { Icon: FileCode2, badge, kind: "code" }
  }
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext)) {
    return { Icon: FileArchive, badge, kind: "archive" }
  }
  if (["txt", "md", "log", "ini", "conf"].includes(ext)) {
    return { Icon: FileText, badge, kind: "text" }
  }

  return { Icon: File, badge, kind: "generic" }
}

export function FileBrowser({
  openListBaseURL,
  listFilesFn = listFiles,
}: FileBrowserProps) {
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const [currentPath, setCurrentPath] = useState(getPathFromUrl)
  const [refreshTick, setRefreshTick] = useState(0)
  const [sort, setSort] = useState<FileSort>(defaultFileSort)
  const [operation, setOperation] = useState<PendingOperation | null>(null)
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(() => new Set())
  const [pendingDelete, setPendingDelete] =
    useState<PendingDeleteOperation | null>(null)
  const [batchRenameOpen, setBatchRenameOpen] = useState(false)
  const selectionDragRef = useRef<SelectionDragState | null>(null)
  const suppressSelectionChangeRef = useRef(false)
  const ignoreNextDirectoryClickRef = useRef(false)

  function clearSelection() {
    setSelectedKeys(new Set())
  }

  useEffect(() => {
    function handlePopState() {
      clearSelection()
      setCurrentPath(getPathFromUrl())
    }

    window.addEventListener("popstate", handlePopState)
    return () => window.removeEventListener("popstate", handlePopState)
  }, [])

  function navigate(path: string) {
    const normalized = normalizeOpenListPath(path)
    clearSelection()
    setPathInUrl(normalized)
    setCurrentPath(normalized)
  }

  function refreshFiles() {
    clearSelection()
    setRefreshTick((value) => value + 1)
  }

  const toggleSort = useCallback((key: FileSortKey) => {
    setSort((current) =>
      current.key === key
        ? { key, direction: current.direction === "asc" ? "desc" : "asc" }
        : { key, direction: defaultSortDirections[key] },
    )
  }, [])

  const selectSort = useCallback((nextSort: FileSort) => {
    setSort(nextSort)
  }, [])

  const query = useQuery({
    queryKey: ["files", currentPath, refreshTick],
    queryFn: () =>
      listFilesFn({
        path: currentPath,
        refresh: refreshTick > 0,
      }),
  })

  const fileOperation = useMutation({
    mutationFn: async ({
      operation: pendingOperation,
      value,
    }: { operation: PendingOperation; value: string }) => {
      if (pendingOperation.kind === "rename") {
        await renameFile({
          path: joinOpenListPath(currentPath, pendingOperation.obj.name),
          name: value,
        })
        return
      }
      const payload = {
        src_dir: currentPath,
        dst_dir: normalizeOpenListPath(value),
        names: pendingOperation.objects.map((item) => item.name),
      }
      if (pendingOperation.kind === "move") {
        await moveFiles(payload)
        return
      }
      await copyFiles(payload)
    },
    onSuccess: async (_data, variables) => {
      toast.success(t("files.operationCompleted"))
      if (variables.operation.kind !== "rename" && variables.operation.bulk) {
        clearSelection()
      }
      setOperation(null)
      await queryClient.invalidateQueries({ queryKey: ["files"] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (pending: PendingDeleteOperation) =>
      removeFiles({
        dir: currentPath,
        names: pending.objects.map((item) => item.name),
      }),
    onSuccess: async (_data, pending) => {
      toast.success(t("files.operationCompleted"))
      setPendingDelete(null)
      if (pending.bulk) {
        clearSelection()
      }
      await queryClient.invalidateQueries({ queryKey: ["files"] })
    },
  })

  function openDelete(objects: OpenListObject[], bulk: boolean) {
    deleteMutation.reset()
    setPendingDelete({ objects, bulk })
  }

  const batchRenameOperation = useMutation({
    mutationFn: (renameObjects: RenameObject[]) =>
      batchRenameFiles({ src_dir: currentPath, rename_objects: renameObjects }),
    onSuccess: async () => {
      toast.success(t("files.operationCompleted"))
      setBatchRenameOpen(false)
      clearSelection()
      await queryClient.invalidateQueries({ queryKey: ["files"] })
    },
  })

  const items = useMemo<FileRowData[]>(
    () =>
      sortObjects(query.data?.content ?? [], sort, locale).map((item) => ({
        ...item,
        key: `${item.is_dir ? "d" : "f"}-${item.name}`,
      })),
    [locale, query.data?.content, sort],
  )

  const selectedItems = useMemo(
    () => items.filter((item) => selectedKeys.has(item.key)),
    [items, selectedKeys],
  )
  const selectionKeyOrder = useMemo(() => items.map((item) => item.key), [items])
  const selectionKeyIndexes = useMemo(
    () => new Map(selectionKeyOrder.map((key, index) => [key, index])),
    [selectionKeyOrder],
  )
  const allSelected = items.length > 0 && selectedItems.length === items.length
  const selectionIndeterminate = selectedItems.length > 0 && !allSelected

  useEffect(() => {
    const availableKeys = new Set(items.map((item) => item.key))
    setSelectedKeys((current) => {
      const next = new Set([...current].filter((key) => availableKeys.has(key)))
      return next.size === current.size ? current : next
    })
  }, [items])

  const toggleSelection = useCallback((key: string, checked: boolean) => {
    setSelectedKeys((current) => {
      const next = new Set(current)
      if (checked) {
        next.add(key)
      } else {
        next.delete(key)
      }
      return next
    })
  }, [])

  const handleSelectionChange = useCallback(
    (key: string, checked: boolean) => {
      if (suppressSelectionChangeRef.current) {
        suppressSelectionChangeRef.current = false
        return
      }
      toggleSelection(key, checked)
    },
    [toggleSelection],
  )

  const updateDragSelection = useCallback((key: string, force = false) => {
    const drag = selectionDragRef.current
    const targetIndex = drag?.keyIndexes.get(key)
    if (!drag?.moved || targetIndex === undefined) {
      return
    }
    if (!force && drag.currentIndex === targetIndex) {
      return
    }

    drag.currentIndex = targetIndex
    const firstIndex = Math.min(drag.anchorIndex, targetIndex)
    const lastIndex = Math.max(drag.anchorIndex, targetIndex)
    setSelectedKeys(() => {
      const next = new Set(drag.initialSelection)
      for (let index = firstIndex; index <= lastIndex; index += 1) {
        const selectionKey = drag.orderedKeys[index]
        if (drag.targetChecked) {
          next.add(selectionKey)
        } else {
          next.delete(selectionKey)
        }
      }
      return next
    })
  }, [])

  const startSelectionDrag = useCallback(
    (key: string, event: ReactPointerEvent<HTMLButtonElement>) => {
      if (event.pointerType === "mouse" && event.button !== 0) {
        return
      }
      const anchorIndex = selectionKeyIndexes.get(key)
      if (anchorIndex === undefined) {
        return
      }
      const initialSelection = new Set(selectedKeys)
      selectionDragRef.current = {
        anchorIndex,
        currentIndex: anchorIndex,
        initialSelection,
        keyIndexes: selectionKeyIndexes,
        moved: false,
        orderedKeys: selectionKeyOrder,
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        targetChecked: !initialSelection.has(key),
      }
    },
    [selectedKeys, selectionKeyIndexes, selectionKeyOrder],
  )

  useEffect(() => {
    function handlePointerMove(event: globalThis.PointerEvent) {
      const drag = selectionDragRef.current
      if (!drag || drag.pointerId !== event.pointerId) {
        return
      }

      if (!drag.moved) {
        const distance = Math.hypot(
          event.clientX - drag.startX,
          event.clientY - drag.startY,
        )
        if (distance < selectionDragThreshold) {
          return
        }
        drag.moved = true
        updateDragSelection(drag.orderedKeys[drag.anchorIndex], true)
        event.preventDefault()
      }

      const target = document.elementFromPoint?.(event.clientX, event.clientY)
      const key = target?.closest<HTMLElement>("[data-selection-key]")?.dataset.selectionKey
      if (key) {
        updateDragSelection(key)
      }
    }

    function finishPointer(event: globalThis.PointerEvent) {
      const drag = selectionDragRef.current
      if (!drag || drag.pointerId !== event.pointerId) {
        return
      }
      if (drag.moved) {
        ignoreNextDirectoryClickRef.current = true
        suppressSelectionChangeRef.current = true
        window.setTimeout(() => {
          suppressSelectionChangeRef.current = false
          ignoreNextDirectoryClickRef.current = false
        }, 0)
      }
      selectionDragRef.current = null
    }

    document.addEventListener("pointermove", handlePointerMove, { passive: false })
    document.addEventListener("pointerup", finishPointer)
    document.addEventListener("pointercancel", finishPointer)
    return () => {
      document.removeEventListener("pointermove", handlePointerMove)
      document.removeEventListener("pointerup", finishPointer)
      document.removeEventListener("pointercancel", finishPointer)
    }
  }, [updateDragSelection])

  function toggleAll(checked: boolean) {
    setSelectedKeys(checked ? new Set(items.map((item) => item.key)) : new Set())
  }

  function openItemAction(kind: OperationKind, obj: OpenListObject) {
    fileOperation.reset()
    if (kind === "rename") {
      setOperation({ kind, obj })
      return
    }
    setOperation({ kind, objects: [obj], bulk: false })
  }

  function openBulkTransfer(kind: TransferKind) {
    fileOperation.reset()
    setOperation({ kind, objects: selectedItems, bulk: true })
  }

  function openDirectory(obj: OpenListObject) {
    if (ignoreNextDirectoryClickRef.current) {
      ignoreNextDirectoryClickRef.current = false
      return
    }
    navigate(joinOpenListPath(currentPath, obj.name))
  }

  const breadcrumbRoutes = useMemo(() => {
    const segments = currentPath.split("/").filter(Boolean)
    const routes = [{ name: t("files.home"), path: "/" }]
    let path = ""
    for (const segment of segments) {
      path = `${path}/${segment}`
      routes.push({ name: segment, path })
    }
    return routes
  }, [currentPath, t])

  const renameOperation = operation?.kind === "rename" ? operation : null
  const transferOperation =
    operation?.kind === "move" || operation?.kind === "copy" ? operation : null

  return (
    <>
      <Card className="w-full gap-0 overflow-hidden py-0 shadow-sm">
        <CardHeader className="flex min-h-20 flex-row flex-wrap items-center gap-4 border-b px-5 py-4 md:px-6">
          <div className="min-w-0 flex-1">
            <CardTitle>{t("files.title")}</CardTitle>
            <FileBreadcrumb routes={breadcrumbRoutes} onNavigate={navigate} />
          </div>
          <CardAction className="flex max-w-full flex-wrap items-center justify-end gap-2">
            <ItemCountBadge total={query.data?.total} />
            <CreateFolderButton
              parentPath={currentPath}
              listFilesFn={listFilesFn}
              disabled={!query.isSuccess}
            />
            <Button onClick={refreshFiles} disabled={query.isFetching}>
              {query.isFetching ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RefreshCw data-icon="inline-start" />
              )}
              {t("files.refresh")}
            </Button>
          </CardAction>
        </CardHeader>

        <CardContent className={cn("p-0", selectedItems.length > 0 && "pb-24")}>
          {query.isLoading ? <FileSkeleton /> : null}
          {query.isError ? (
            <ErrorState error={query.error} onRetry={() => void query.refetch()} />
          ) : null}
          {query.isSuccess && items.length === 0 ? <EmptyState /> : null}
          {query.isSuccess && items.length > 0 ? (
            <>
              <DesktopFileTable
                allSelected={allSelected}
                currentPath={currentPath}
                indeterminate={selectionIndeterminate}
                items={items}
                locale={locale}
                openListBaseURL={openListBaseURL}
                selectedKeys={selectedKeys}
                sort={sort}
                onAction={openItemAction}
                onDelete={(obj) => openDelete([obj], false)}
                onOpenDirectory={openDirectory}
                onSelect={handleSelectionChange}
                onSelectDragStart={startSelectionDrag}
                onSelectDragEnter={updateDragSelection}
                onSelectAll={toggleAll}
                onSort={toggleSort}
              />
              <MobileFileList
                allSelected={allSelected}
                currentPath={currentPath}
                indeterminate={selectionIndeterminate}
                items={items}
                locale={locale}
                openListBaseURL={openListBaseURL}
                selectedKeys={selectedKeys}
                sort={sort}
                onAction={openItemAction}
                onDelete={(obj) => openDelete([obj], false)}
                onOpenDirectory={openDirectory}
                onSelect={handleSelectionChange}
                onSelectDragStart={startSelectionDrag}
                onSelectDragEnter={updateDragSelection}
                onSelectAll={toggleAll}
                onSortChange={selectSort}
              />
            </>
          ) : null}
        </CardContent>
      </Card>

      <RenameDialog
        error={fileOperation.error}
        isPending={fileOperation.isPending}
        operation={renameOperation}
        onClose={() => {
          fileOperation.reset()
          setOperation(null)
        }}
        onSubmit={(value) => {
          if (renameOperation) {
            fileOperation.mutate({ operation: renameOperation, value })
          }
        }}
      />
      <TransferDialog
        count={transferOperation?.objects.length ?? 0}
        currentPath={currentPath}
        error={fileOperation.error}
        isPending={fileOperation.isPending}
        kind={transferOperation?.kind ?? null}
        listFilesFn={listFilesFn}
        objects={transferOperation?.objects ?? []}
        open={Boolean(transferOperation)}
        onClose={() => {
          fileOperation.reset()
          setOperation(null)
        }}
        onSubmit={(destination) => {
          if (transferOperation) {
            fileOperation.mutate({ operation: transferOperation, value: destination })
          }
        }}
      />
      <DeleteDialog
        count={pendingDelete?.objects.length ?? 0}
        error={deleteMutation.error}
        isPending={deleteMutation.isPending}
        open={pendingDelete !== null}
        onClose={() => {
          deleteMutation.reset()
          setPendingDelete(null)
        }}
        onConfirm={() => pendingDelete && deleteMutation.mutate(pendingDelete)}
      />
      <BatchRenameDialog
        allItems={items}
        error={batchRenameOperation.error}
        isPending={batchRenameOperation.isPending}
        open={batchRenameOpen}
        selectedItems={selectedItems}
        onClose={() => {
          batchRenameOperation.reset()
          setBatchRenameOpen(false)
        }}
        onSubmit={(renameObjects) => batchRenameOperation.mutate(renameObjects)}
      />
      {selectedItems.length > 0 ? (
        <BulkActionBar
          count={selectedItems.length}
          onClear={clearSelection}
          onCopy={() => openBulkTransfer("copy")}
          onDelete={() => openDelete(selectedItems, true)}
          onMove={() => openBulkTransfer("move")}
          onRename={() => {
            batchRenameOperation.reset()
            setBatchRenameOpen(true)
          }}
        />
      ) : null}
    </>
  )
}

function FileBreadcrumb({
  routes,
  onNavigate,
}: {
  routes: Array<{ name: string; path: string }>
  onNavigate: (path: string) => void
}) {
  return (
    <Breadcrumb className="mt-1.5">
      <BreadcrumbList>
        {routes.map((route, index) => {
          const isCurrent = index === routes.length - 1
          return (
            <Fragment key={route.path}>
              <BreadcrumbItem>
                {isCurrent ? (
                  <BreadcrumbPage>{route.name}</BreadcrumbPage>
                ) : (
                  <BreadcrumbLink asChild>
                    <button type="button" onClick={() => onNavigate(route.path)}>
                      {route.name}
                    </button>
                  </BreadcrumbLink>
                )}
              </BreadcrumbItem>
              {!isCurrent ? <BreadcrumbSeparator /> : null}
            </Fragment>
          )
        })}
      </BreadcrumbList>
    </Breadcrumb>
  )
}

function ItemCountBadge({ total }: { total?: number }) {
  const { t } = useI18n()
  return (
    <div className="hidden items-center gap-2 lg:flex">
      <Badge variant="secondary">
        {total === undefined ? t("files.ready") : t("files.items", { count: total })}
      </Badge>
    </div>
  )
}

function SelectionCheckbox({
  checked,
  label,
  onDragStart,
  onChange,
}: {
  checked: boolean | "indeterminate"
  label: string
  onDragStart?: (event: ReactPointerEvent<HTMLButtonElement>) => void
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="flex size-11 items-center justify-center">
      <Checkbox
        checked={checked}
        aria-label={label}
        className="relative touch-none after:absolute after:-inset-3 after:content-['']"
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => {
          event.stopPropagation()
          onDragStart?.(event)
        }}
        onCheckedChange={(value) => onChange(value === true)}
      />
    </div>
  )
}

function SortIndicator({ direction, isActive }: { direction: FileSortDirection; isActive: boolean }) {
  if (!isActive) {
    return <ArrowUpDown aria-hidden="true" className="size-4 text-muted-foreground/70" />
  }
  return direction === "asc" ? (
    <ArrowUp aria-hidden="true" className="size-4" />
  ) : (
    <ArrowDown aria-hidden="true" className="size-4" />
  )
}

function SortableTableHead({
  className,
  label,
  sort,
  sortKey,
  onSort,
}: {
  className?: string
  label: string
  sort: FileSort
  sortKey: FileSortKey
  onSort: (key: FileSortKey) => void
}) {
  const { t } = useI18n()
  const isActive = sort.key === sortKey

  return (
    <TableHead
      aria-sort={isActive ? (sort.direction === "asc" ? "ascending" : "descending") : "none"}
      className={cn("h-11", className)}
    >
      <Button
        type="button"
        aria-label={t("files.sortBy", { column: label })}
        aria-pressed={isActive}
        className={cn(
          "h-11 gap-1.5 px-2 text-muted-foreground hover:text-foreground",
          isActive && "text-foreground",
        )}
        variant="ghost"
        onClick={() => onSort(sortKey)}
      >
        <span>{label}</span>
        <SortIndicator direction={sort.direction} isActive={isActive} />
      </Button>
    </TableHead>
  )
}

function FileSortMenu({
  sort,
  onSortChange,
}: {
  sort: FileSort
  onSortChange: (sort: FileSort) => void
}) {
  const { t } = useI18n()
  const value = `${sort.key}:${sort.direction}`

  return (
    <Tooltip>
      <DropdownMenu>
        <TooltipTrigger asChild>
          <span className="inline-flex">
            <DropdownMenuTrigger asChild>
              <Button type="button" aria-label={t("files.sort")} size="icon-lg" variant="ghost">
                <ArrowUpDown />
              </Button>
            </DropdownMenuTrigger>
          </span>
        </TooltipTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuRadioGroup
            value={value}
            onValueChange={(nextValue) => {
              const option = fileSortOptions.find((item) => item.value === nextValue)
              if (option) {
                onSortChange({ key: option.key, direction: option.direction })
              }
            }}
          >
            {fileSortOptions.map((option) => (
              <DropdownMenuRadioItem
                className="min-h-11 px-2.5"
                key={option.value}
                value={option.value}
              >
                {t(option.labelKey)}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <TooltipContent>{t("files.sort")}</TooltipContent>
    </Tooltip>
  )
}

function DesktopFileTable({
  allSelected,
  currentPath,
  indeterminate,
  items,
  locale,
  openListBaseURL,
  selectedKeys,
  sort,
  onAction,
  onDelete,
  onOpenDirectory,
  onSelect,
  onSelectDragEnter,
  onSelectDragStart,
  onSelectAll,
  onSort,
}: {
  allSelected: boolean
  currentPath: string
  indeterminate: boolean
  items: FileRowData[]
  locale: string
  openListBaseURL?: string
  selectedKeys: Set<string>
  sort: FileSort
  onAction: (kind: OperationKind, obj: OpenListObject) => void
  onDelete: (obj: OpenListObject) => void
  onOpenDirectory: (obj: OpenListObject) => void
  onSelect: (key: string, checked: boolean) => void
  onSelectDragEnter: (key: string) => void
  onSelectDragStart: (
    key: string,
    event: ReactPointerEvent<HTMLButtonElement>,
  ) => void
  onSelectAll: (checked: boolean) => void
  onSort: (key: FileSortKey) => void
}) {
  const { t } = useI18n()

  return (
    <div className="hidden md:block">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="h-11 w-14 px-3 md:pl-4">
              <SelectionCheckbox
                checked={allSelected ? true : indeterminate ? "indeterminate" : false}
                label={t("files.selectAll")}
                onChange={onSelectAll}
              />
            </TableHead>
            <SortableTableHead
              className="px-2"
              label={t("files.name")}
              sort={sort}
              sortKey="name"
              onSort={onSort}
            />
            <SortableTableHead
              className="w-36"
              label={t("files.size")}
              sort={sort}
              sortKey="size"
              onSort={onSort}
            />
            <SortableTableHead
              className="w-52"
              label={t("files.modified")}
              sort={sort}
              sortKey="modified"
              onSort={onSort}
            />
            <TableHead className="w-72 pr-5 text-right md:pr-6">
              {t("files.actions")}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((obj) => {
            const isSelected = selectedKeys.has(obj.key)
            return (
              <TableRow
                key={obj.key}
                data-state={isSelected ? "selected" : undefined}
                data-selection-key={obj.key}
                className={cn(
                  "data-[state=selected]:bg-primary/5 data-[state=selected]:hover:bg-primary/10",
                  obj.is_dir &&
                    "cursor-pointer focus-visible:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
                )}
                tabIndex={obj.is_dir ? 0 : undefined}
                onPointerEnter={() => onSelectDragEnter(obj.key)}
                aria-label={obj.is_dir ? t("files.openFolder", { name: obj.name }) : undefined}
                onClick={obj.is_dir ? () => onOpenDirectory(obj) : undefined}
                onKeyDown={
                  obj.is_dir
                    ? (event) => {
                        if (
                          event.target === event.currentTarget &&
                          (event.key === "Enter" || event.key === " ")
                        ) {
                          event.preventDefault()
                          onOpenDirectory(obj)
                        }
                      }
                    : undefined
                }
              >
                <TableCell className="w-14 px-3 md:pl-4">
                  <SelectionCheckbox
                    checked={isSelected}
                    label={t("files.selectName", { name: obj.name })}
                    onDragStart={(event) => onSelectDragStart(obj.key, event)}
                    onChange={(checked) => onSelect(obj.key, checked)}
                  />
                </TableCell>
                <TableCell className="px-2 py-3">
                  <FileNameCell obj={obj} />
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {obj.is_dir ? t("files.folder") : formatBytes(obj.size)}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {formatDate(obj.modified, locale)}
                </TableCell>
                <TableCell className="pr-5 md:pr-6">
                  <FileActions
                    downloadUrl={buildDownloadUrl(currentPath, obj, openListBaseURL)}
                    obj={obj}
                    onAction={(kind) => onAction(kind, obj)}
                    onDelete={() => onDelete(obj)}
                  />
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function FileNameCell({ obj }: { obj: OpenListObject }) {
  const { t } = useI18n()

  return (
    <div className="flex min-w-0 items-center gap-3">
      <FileIcon obj={obj} />
      <div className="min-w-0">
        <div className="truncate text-sm font-medium" title={obj.name}>
          {obj.name}
        </div>
        <div className="mt-0.5 text-xs text-muted-foreground">
          {obj.is_dir ? t("files.directory") : t("files.file")}
        </div>
      </div>
    </div>
  )
}

function FileIcon({ obj }: { obj: OpenListObject }) {
  const visual = getFileVisual(obj)
  const Icon = visual.Icon

  return (
    <div
      className={cn(
        "relative flex size-10 shrink-0 items-center justify-center rounded-xl ring-1",
        fileToneClassNames[visual.kind],
      )}
    >
      <Icon className="size-5" strokeWidth={1.8} />
      {visual.badge ? (
        <span className="absolute right-0.5 bottom-0.5 max-w-7 truncate rounded bg-card px-1 text-[7px] leading-3 font-bold ring-1 ring-border">
          {visual.badge}
        </span>
      ) : null}
    </div>
  )
}

function FileActions({
  ariaContext = "",
  downloadUrl,
  obj,
  onAction,
  onDelete,
}: {
  ariaContext?: string
  downloadUrl: string | null
  obj: OpenListObject
  onAction: (kind: OperationKind) => void
  onDelete: () => void
}) {
  const { t } = useI18n()

  return (
    <div
      className="flex items-center justify-end gap-1"
      aria-label={`${t("files.actionsFor", { name: obj.name })}${ariaContext}`}
      onClick={(event) => event.stopPropagation()}
    >
      <FileActionButton
        ariaLabel={`${t("files.renameName", { name: obj.name })}${ariaContext}`}
        icon={Pencil}
        tooltip={t("files.rename")}
        onClick={() => onAction("rename")}
      />
      <FileActionButton
        ariaLabel={`${t("files.copyName", { name: obj.name })}${ariaContext}`}
        icon={Copy}
        tooltip={t("files.copy")}
        onClick={() => onAction("copy")}
      />
      <FileActionButton
        ariaLabel={`${t("files.moveName", { name: obj.name })}${ariaContext}`}
        icon={ArrowRight}
        tooltip={t("files.move")}
        onClick={() => onAction("move")}
      />
      {downloadUrl ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-lg" asChild>
              <a
                download
                href={downloadUrl}
                aria-label={`${t("files.downloadName", { name: obj.name })}${ariaContext}`}
              >
                <Download />
              </a>
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("files.download")}</TooltipContent>
        </Tooltip>
      ) : null}
      <FileActionButton
        ariaLabel={`${t("files.deleteName", { name: obj.name })}${ariaContext}`}
        className="text-destructive hover:bg-destructive/10 hover:text-destructive"
        icon={Trash2}
        tooltip={t("files.delete")}
        onClick={onDelete}
      />
    </div>
  )
}

function FileActionButton({
  ariaLabel,
  className,
  icon: Icon,
  tooltip,
  onClick,
}: {
  ariaLabel: string
  className?: string
  icon: LucideIcon
  tooltip: string
  onClick: () => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          aria-label={ariaLabel}
          className={className}
          variant="ghost"
          size="icon-lg"
          onClick={onClick}
        >
          <Icon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  )
}

function MobileFileList({
  allSelected,
  currentPath,
  indeterminate,
  items,
  locale,
  openListBaseURL,
  selectedKeys,
  sort,
  onAction,
  onDelete,
  onOpenDirectory,
  onSelect,
  onSelectDragEnter,
  onSelectDragStart,
  onSelectAll,
  onSortChange,
}: {
  allSelected: boolean
  currentPath: string
  indeterminate: boolean
  items: FileRowData[]
  locale: string
  openListBaseURL?: string
  selectedKeys: Set<string>
  sort: FileSort
  onAction: (kind: OperationKind, obj: OpenListObject) => void
  onDelete: (obj: OpenListObject) => void
  onOpenDirectory: (obj: OpenListObject) => void
  onSelect: (key: string, checked: boolean) => void
  onSelectDragEnter: (key: string) => void
  onSelectDragStart: (
    key: string,
    event: ReactPointerEvent<HTMLButtonElement>,
  ) => void
  onSelectAll: (checked: boolean) => void
  onSortChange: (sort: FileSort) => void
}) {
  const { t } = useI18n()

  return (
    <div className="md:hidden">
      <div className="flex min-h-12 items-center gap-2 border-b px-3 text-sm">
        <SelectionCheckbox
          checked={allSelected ? true : indeterminate ? "indeterminate" : false}
          label={t("files.selectAll")}
          onChange={onSelectAll}
        />
        <span className="font-medium">{t("files.selectAll")}</span>
        <div className="ml-auto flex items-center gap-1">
          {selectedKeys.size > 0 ? (
            <span className="text-muted-foreground">
              {t("files.selectedCount", { count: selectedKeys.size })}
            </span>
          ) : null}
          <FileSortMenu sort={sort} onSortChange={onSortChange} />
        </div>
      </div>
      <div className="divide-y">
        {items.map((obj) => {
          const isSelected = selectedKeys.has(obj.key)
          return (
            <article
              className={cn(
                "flex flex-col gap-3 p-4 transition-colors",
                isSelected && "bg-primary/5",
                obj.is_dir &&
                  "cursor-pointer focus-visible:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
              )}
              key={obj.key}
              data-selection-key={obj.key}
              tabIndex={obj.is_dir ? 0 : undefined}
              onPointerEnter={() => onSelectDragEnter(obj.key)}
              aria-label={
                obj.is_dir ? t("files.openFolderMobile", { name: obj.name }) : undefined
              }
              onClick={obj.is_dir ? () => onOpenDirectory(obj) : undefined}
              onKeyDown={
                obj.is_dir
                  ? (event) => {
                      if (
                        event.target === event.currentTarget &&
                        (event.key === "Enter" || event.key === " ")
                      ) {
                        event.preventDefault()
                        onOpenDirectory(obj)
                      }
                    }
                  : undefined
              }
            >
              <div className="flex min-w-0 items-center gap-3">
                <SelectionCheckbox
                  checked={isSelected}
                  label={t("files.selectName", { name: obj.name })}
                  onDragStart={(event) => onSelectDragStart(obj.key, event)}
                  onChange={(checked) => onSelect(obj.key, checked)}
                />
                <FileIcon obj={obj} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{obj.name}</div>
                  <div className="mt-1 truncate text-xs text-muted-foreground">
                    {obj.is_dir ? t("files.folder") : formatBytes(obj.size)} · {formatDate(obj.modified, locale)}
                  </div>
                </div>
              </div>
              <FileActions
                ariaContext=" mobile"
                downloadUrl={buildDownloadUrl(currentPath, obj, openListBaseURL)}
                obj={obj}
                onAction={(kind) => onAction(kind, obj)}
                onDelete={() => onDelete(obj)}
              />
            </article>
          )
        })}
      </div>
    </div>
  )
}

function RenameDialog({
  error,
  isPending,
  operation,
  onClose,
  onSubmit,
}: {
  error: Error | null
  isPending: boolean
  operation: PendingRenameOperation | null
  onClose: () => void
  onSubmit: (value: string) => void
}) {
  const { t } = useI18n()
  const [value, setValue] = useState("")

  useEffect(() => {
    if (!operation) {
      setValue("")
      return
    }
    setValue(operation.obj.name)
  }, [operation])

  const title = t("files.renameItem")

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextValue = value.trim()
    if (nextValue) {
      onSubmit(nextValue)
    }
  }

  return (
    <Dialog open={Boolean(operation)} onOpenChange={(open) => !open && !isPending && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="px-4 pt-4 sm:px-6 sm:pt-6">
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className="truncate">
            {operation?.obj.name}
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="overscroll-contain px-4 pb-4 sm:px-6">
          <form id="rename-file-form" onSubmit={handleSubmit}>
            <FieldGroup>
              <Field data-invalid={Boolean(error)}>
                <FieldLabel htmlFor="file-operation-value">{t("files.newName")}</FieldLabel>
                <Input
                  id="file-operation-value"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                  placeholder={t("files.newFileName")}
                  aria-invalid={Boolean(error)}
                  autoFocus
                  required
                />
                {error ? <FieldError>{error.message}</FieldError> : null}
              </Field>
              {operation && supportsMediaSearch(operation.obj) ? (
                <MediaSearchPanel
                  originalName={operation.obj.name}
                  isDirectory={operation.obj.is_dir}
                  onSelectName={setValue}
                />
              ) : null}
            </FieldGroup>
          </form>
        </DialogBody>
        <DialogFooter className="mx-0 mb-0 rounded-none px-4 sm:px-6">
          <Button type="button" variant="outline" onClick={onClose} disabled={isPending}>
            {t("settings.cancel")}
          </Button>
          <Button
            type="submit"
            form="rename-file-form"
            aria-label={title}
            disabled={isPending || !value.trim()}
          >
            {isPending ? <Spinner data-icon="inline-start" /> : null}
            {title}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function FileSkeleton() {
  const { t } = useI18n()
  return (
    <div className="flex flex-col gap-3 p-5 md:p-6" aria-label={t("files.loading")}>
      {Array.from({ length: 6 }).map((_, index) => (
        <div className="flex items-center gap-3" key={index}>
          <Skeleton className="size-10 shrink-0 rounded-xl" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-48 max-w-full" />
            <Skeleton className="h-3 w-24" />
          </div>
        </div>
      ))}
    </div>
  )
}

function ErrorState({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const { t } = useI18n()
  const code = error instanceof ManagerApiError ? error.code : undefined
  const authHint =
    code === 401 || code === 403 ? t("files.authHint") : t("files.genericHint")

  return (
    <div className="p-5 md:p-6">
      <Alert>
        <TriangleAlert />
        <AlertTitle>{t("files.errorTitle")}</AlertTitle>
        <AlertDescription className="flex flex-col items-start gap-3">
          <span>{error.message}</span>
          <span>{authHint}</span>
          <Button variant="outline" onClick={onRetry}>
            <RefreshCw data-icon="inline-start" />
            {t("files.retry")}
          </Button>
        </AlertDescription>
      </Alert>
    </div>
  )
}

function EmptyState() {
  const { t } = useI18n()
  return (
    <Empty className="min-h-72">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FolderOpen />
        </EmptyMedia>
        <EmptyTitle>{t("files.emptyTitle")}</EmptyTitle>
        <EmptyDescription>{t("files.emptyDescription")}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
