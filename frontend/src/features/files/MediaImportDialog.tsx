import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { LibraryBig } from "lucide-react"
import { toast } from "sonner"

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
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { taskQueryKeys } from "@/features/tasks/queryKeys"
import {
  importMediaLibraryPaths,
  joinOpenListPath,
  listAllMediaLibraries,
  normalizeOpenListPath,
  type OpenListObject,
  type TaskRun,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

interface MediaImportDialogProps {
  currentPath: string
  objects: OpenListObject[]
  onClose: () => void
  onCreated: (run: TaskRun) => void
}

export function MediaImportDialog({
  currentPath,
  objects,
  onClose,
  onCreated,
}: MediaImportDialogProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [chosenLibraryID, setChosenLibraryID] = useState<string | null>(null)
  const paths = objects.map((object) => joinOpenListPath(currentPath, object.name))
  const librariesQuery = useQuery({
    queryKey: ["media-libraries", "all"],
    queryFn: listAllMediaLibraries,
  })
  const libraries = (librariesQuery.data?.items ?? []).filter((library) => {
    const roots = library.root_paths?.length
      ? library.root_paths
      : library.root_path ? [library.root_path] : []
    return paths.every((path) =>
      roots.some((root) => {
        const normalizedRoot = normalizeOpenListPath(root)
        return path === normalizedRoot ||
          path.startsWith(`${normalizedRoot === "/" ? "" : normalizedRoot}/`)
      }),
    )
  })
  const libraryID = chosenLibraryID ?? (libraries.length === 1 ? String(libraries[0].id) : "")
  const selectedLibrary = libraries.find((library) => String(library.id) === libraryID)
  const mutation = useMutation({
    mutationFn: (id: number) => importMediaLibraryPaths(id, paths),
    onSuccess: (run) => {
      void queryClient.invalidateQueries({ queryKey: taskQueryKeys.all })
      toast.success(t("files.importMediaStarted"))
      onCreated(run)
    },
  })

  return (
    <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("files.importMedia")}</DialogTitle>
          <DialogDescription>{t("files.selectedCount", { count: paths.length })}</DialogDescription>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <ul className="max-h-40 space-y-1 overflow-y-auto rounded-lg border p-3 text-sm">
            {paths.map((path) => <li key={path} className="break-all">{path}</li>)}
          </ul>
          {librariesQuery.isPending ? <Skeleton className="h-11 w-full" aria-label={t("common.loading")} /> : null}
          {librariesQuery.isError ? (
            <div role="alert" className="space-y-2">
              <FieldError>{t("mediaLibrary.loadFailed")}</FieldError>
              <Button variant="outline" onClick={() => void librariesQuery.refetch()}>{t("files.retry")}</Button>
            </div>
          ) : null}
          {librariesQuery.isSuccess && libraries.length === 0 ? (
            <FieldError role="alert">{t("files.importMediaNoLibrary")}</FieldError>
          ) : null}
          {librariesQuery.isSuccess && libraries.length > 0 ? (
            <Field>
              <FieldLabel htmlFor="media-import-library">{t("files.importMediaLibrary")}</FieldLabel>
              <Select value={libraryID} onValueChange={setChosenLibraryID} disabled={mutation.isPending}>
                <SelectTrigger id="media-import-library" className="min-h-11 w-full min-w-0">
                  <SelectValue placeholder={t("files.importMediaLibrary")} />
                </SelectTrigger>
                <SelectContent position="popper" className="w-(--radix-select-trigger-width) max-w-[calc(100vw-2rem)]">
                  {libraries.map((library) => (
                    <SelectItem key={library.id} value={String(library.id)} className="min-h-11 [&>span:last-child]:min-w-0">
                      <span className="truncate" title={library.name}>{library.name}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          {mutation.isError ? <FieldError role="alert">{t("files.importMediaFailed")}</FieldError> : null}
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" disabled={mutation.isPending} onClick={onClose}>{t("settings.cancel")}</Button>
          <Button
            disabled={!selectedLibrary || mutation.isPending || librariesQuery.isError}
            onClick={() => selectedLibrary && mutation.mutate(selectedLibrary.id)}
          >
            {mutation.isPending ? <Spinner /> : <LibraryBig />}
            {t("files.importMedia")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
