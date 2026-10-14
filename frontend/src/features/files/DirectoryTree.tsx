import { useEffect, useMemo, useRef, useState } from "react"
import {
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  FolderPlus,
  MoreHorizontal,
  RefreshCw,
} from "lucide-react"
import { useQuery } from "@tanstack/react-query"

import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuTrigger, ContextMenuContent, ContextMenuItem } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import { joinOpenListPath, listFiles } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

type CreateDirectory = (path: string, trigger: HTMLButtonElement) => void

export function DirectoryTree({
  ariaLabel,
  isDisabled,
  listFilesFn = listFiles,
  selectedPath,
  selectedPaths,
  revealPath,
  onSelect,
  onCreateFolder,
}: {
  ariaLabel?: string
  isDisabled: (path: string) => boolean
  listFilesFn?: typeof listFiles
  selectedPath: string | null
  selectedPaths?: readonly string[]
  revealPath?: string | null
  onSelect: (path: string) => void
  onCreateFolder?: CreateDirectory
}) {
  const { t } = useI18n()

  return (
    <div
      className="max-h-[min(52dvh,30rem)] min-h-64 overflow-auto rounded-lg border bg-muted/10 p-2"
      aria-label={ariaLabel ?? t("files.directoryTree")}
    >
      <DirectoryTreeNode
        depth={0}
        initiallyExpanded
        isDisabled={isDisabled}
        listFilesFn={listFilesFn}
        path="/"
        selectedPath={selectedPath}
        selectedPaths={selectedPaths}
        revealPath={revealPath}
        onSelect={onSelect}
        onCreateFolder={onCreateFolder}
      />
    </div>
  )
}

function DirectoryTreeNode({
  depth,
  initiallyExpanded = false,
  isDisabled,
  listFilesFn,
  path,
  selectedPath,
  selectedPaths,
  revealPath,
  onSelect,
  onCreateFolder,
}: {
  depth: number
  initiallyExpanded?: boolean
  isDisabled: (path: string) => boolean
  listFilesFn: typeof listFiles
  path: string
  selectedPath: string | null
  selectedPaths?: readonly string[]
  revealPath?: string | null
  onSelect: (path: string) => void
  onCreateFolder?: CreateDirectory
}) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState(initiallyExpanded)
  const menuButton = useRef<HTMLButtonElement>(null)
  const openingCreation = useRef(false)
  useEffect(() => {
    if (revealPath && (path === "/" || revealPath.startsWith(`${path}/`))) {
      setExpanded(true)
    }
  }, [path, revealPath])
  const query = useQuery({
    queryKey: ["directory-tree", path],
    queryFn: () => listFilesFn({ path }),
    enabled: expanded,
    retry: false,
    staleTime: 0,
  })
  const directories = useMemo(
    () =>
      (query.data?.content ?? [])
        .filter((item) => item.is_dir)
        .sort((left, right) =>
          left.name.localeCompare(right.name, undefined, {
            numeric: true,
            sensitivity: "base",
          }),
        ),
    [query.data?.content],
  )
  const selected = selectedPaths ? selectedPaths.includes(path) : selectedPath === path
  const disabled = isDisabled(path)
  const name =
    path === "/"
      ? t("files.rootDirectory")
      : path.slice(path.lastIndexOf("/") + 1)

  function requestCreation() {
    if (menuButton.current) {
      openingCreation.current = true
      onCreateFolder?.(path, menuButton.current)
    }
  }

  function closeMenu(event: Event) {
    if (openingCreation.current) {
      event.preventDefault()
      openingCreation.current = false
    }
  }

  const canCreate = !!onCreateFolder && !disabled
  const row = (
      <div
        className={cn(
          "group flex min-w-0 items-center rounded-lg transition-colors",
          selected && "bg-primary/10",
        )}
        style={{ paddingLeft: `${depth * 16}px` }}
      >
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          aria-label={
            expanded
              ? t("files.collapseFolder", { name })
              : t("files.expandFolder", { name })
          }
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
        >
          {query.isFetching ? (
            <Spinner />
          ) : expanded ? (
            <ChevronDown />
          ) : (
            <ChevronRight />
          )}
        </Button>
        <Button
          type="button"
          variant="ghost"
          className={cn(
            "min-w-0 flex-1 justify-start px-2 font-normal",
            selected &&
              "font-medium text-primary hover:bg-primary/10 hover:text-primary",
          )}
          aria-label={t("files.selectFolder", { name })}
          aria-pressed={selected}
          disabled={disabled}
          onClick={() => onSelect(path)}
        >
          {expanded ? (
            <FolderOpen data-icon="inline-start" />
          ) : (
            <Folder data-icon="inline-start" />
          )}
          <span className="truncate" title={name}>
            {name}
          </span>
        </Button>
        {canCreate ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button ref={menuButton} type="button" variant="ghost" size="icon-lg"
                className="size-11 shrink-0 [@media(hover:hover)_and_(pointer:fine)]:opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 data-[state=open]:opacity-100"
                aria-label={t("files.folderActions", { name })}>
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onCloseAutoFocus={closeMenu}>
              <DropdownMenuItem className="min-h-11" onSelect={requestCreation}>
                <FolderPlus />{t("files.createFolder")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
  )

  return (
    <div>
      {canCreate ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent onCloseAutoFocus={closeMenu}>
            <ContextMenuItem className="min-h-11" onSelect={requestCreation}>
              <FolderPlus />{t("files.createFolder")}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : row}

      {expanded && query.isLoading ? (
        <DirectoryTreeSkeleton depth={depth + 1} />
      ) : null}
      {expanded && query.isError ? (
        <div
          className="flex min-h-11 items-center gap-2 pr-2 text-sm text-destructive"
          style={{ paddingLeft: `${(depth + 1) * 16 + 44}px` }}
        >
          <span className="min-w-0 flex-1 truncate">
            {t("files.loadFoldersFailed")}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={t("files.retryFolders")}
            onClick={() => void query.refetch()}
          >
            <RefreshCw />
          </Button>
        </div>
      ) : null}
      {expanded && query.isSuccess
        ? directories.map((directory) => (
            <DirectoryTreeNode
              key={directory.name}
              depth={depth + 1}
              isDisabled={isDisabled}
              listFilesFn={listFilesFn}
              path={joinOpenListPath(path, directory.name)}
              selectedPath={selectedPath}
              selectedPaths={selectedPaths}
              revealPath={revealPath}
              onSelect={onSelect}
              onCreateFolder={onCreateFolder}
            />
          ))
        : null}
    </div>
  )
}

function DirectoryTreeSkeleton({ depth }: { depth: number }) {
  return (
    <div
      className="flex flex-col gap-2 py-2"
      style={{ paddingLeft: `${depth * 16 + 44}px` }}
    >
      <Skeleton className="h-8 w-36 max-w-[70%]" />
      <Skeleton className="h-8 w-28 max-w-[60%]" />
    </div>
  )
}
