import { useId, useRef, useState, type FormEvent, type ReactNode } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { FolderPlus } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { createFolder, joinOpenListPath, listFiles } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

export function CreateFolderButton({
  parentPath,
  disabled = false,
  listFilesFn = listFiles,
  onCreated,
  onOpenChange,
}: {
  parentPath: string
  disabled?: boolean
  listFilesFn?: typeof listFiles
  onCreated?: (path: string) => void
  onOpenChange?: (open: boolean) => void
}) {
  const { t } = useI18n()

  return (
    <CreateFolderDialog
      parentPath={parentPath}
      listFilesFn={listFilesFn}
      onCreated={onCreated}
      onOpenChange={onOpenChange}
      trigger={
        <Button type="button" variant="outline" className="min-h-11" disabled={disabled}>
          <FolderPlus data-icon="inline-start" />
          {t("files.createFolder")}
        </Button>
      }
    />
  )
}

export function CreateFolderDialog({
  parentPath,
  open,
  trigger,
  listFilesFn = listFiles,
  onCreated,
  onOpenChange,
  onCloseAutoFocus,
}: {
  parentPath: string | null
  open?: boolean
  trigger?: ReactNode
  onCloseAutoFocus?: (event: Event) => void
  listFilesFn?: typeof listFiles
  onCreated?: (path: string) => void
  onOpenChange?: (open: boolean) => void
}) {
  const [internalOpen, setInternalOpen] = useState(false)
  const controlled = open !== undefined
  const dialogOpen = controlled ? open : internalOpen

  function changeOpen(next: boolean) {
    if (!controlled) setInternalOpen(next)
    onOpenChange?.(next)
  }

  return (
    <Dialog open={dialogOpen} onOpenChange={changeOpen}>
      {trigger ? <DialogTrigger asChild>{trigger}</DialogTrigger> : null}
      {dialogOpen && parentPath !== null ? (
        <CreateFolderForm
          onCloseAutoFocus={onCloseAutoFocus}
          parentPath={parentPath}
          listFilesFn={listFilesFn}
          onCreated={(path) => {
            onCreated?.(path)
            changeOpen(false)
          }}
          onClose={() => changeOpen(false)}
        />
      ) : null}
    </Dialog>
  )
}

function CreateFolderForm({
  parentPath,
  listFilesFn,
  onCreated,
  onClose,
  onCloseAutoFocus,
}: {
  parentPath: string
  listFilesFn: typeof listFiles
  onCreated: (path: string) => void
  onClose: () => void
  onCloseAutoFocus?: (event: Event) => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const id = useId()
  const [name, setName] = useState("")
  const [createdPath, setCreatedPath] = useState<string | null>(null)
  const submitting = useRef(false)
  const trimmedName = name.trim()
  const invalidName =
    trimmedName === "." ||
    trimmedName === ".." ||
    /[/\\]/.test(trimmedName) ||
    Array.from(name).some((char) => {
      const code = char.charCodeAt(0)
      return code < 32 || (code >= 127 && code <= 159)
    })
  const mutation = useMutation({
    mutationFn: async () => {
      const path = createdPath ?? joinOpenListPath(parentPath, trimmedName)
      if (!createdPath) {
        await createFolder({ path })
        // A refresh failure must never retry the successful creation request.
        setCreatedPath(path)
      }
      await queryClient.cancelQueries({ queryKey: ["files", parentPath] })
      await queryClient.cancelQueries({
        queryKey: ["directory-tree", parentPath],
        exact: true,
      })
      await queryClient.invalidateQueries({
        queryKey: ["files", parentPath],
        refetchType: "none",
      })
      await queryClient.invalidateQueries({
        queryKey: ["directory-tree", parentPath],
        exact: true,
        refetchType: "none",
      })
      const listing = await listFilesFn({ path: parentPath, refresh: true })
      queryClient.setQueriesData({ queryKey: ["files", parentPath] }, listing)
      queryClient.setQueryData(["directory-tree", parentPath], listing)
      return path
    },
    onSuccess: onCreated,
    onSettled: () => {
      submitting.current = false
    },
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (submitting.current || (!createdPath && (!trimmedName || invalidName))) {
      return
    }
    submitting.current = true
    mutation.mutate()
  }

  return (
    <DialogContent
      onCloseAutoFocus={onCloseAutoFocus}
      showCloseButton={!mutation.isPending}
      onEscapeKeyDown={(event) => {
        if (mutation.isPending) event.preventDefault()
      }}
      onInteractOutside={(event) => {
        if (mutation.isPending) event.preventDefault()
      }}
    >
      <DialogHeader>
        <DialogTitle>{t("files.createFolder")}</DialogTitle>
        <DialogDescription className="truncate" title={parentPath}>
          {parentPath}
        </DialogDescription>
      </DialogHeader>
      <form id={id} onSubmit={submit}>
        <Field data-invalid={invalidName || Boolean(mutation.error)}>
          <FieldLabel htmlFor={`${id}-name`}>{t("files.folderName")}</FieldLabel>
          <Input
            id={`${id}-name`}
            value={name}
            autoFocus
            required
            disabled={mutation.isPending || Boolean(createdPath)}
            aria-invalid={invalidName}
            aria-describedby={invalidName || mutation.error ? `${id}-error` : undefined}
            onChange={(event) => {
              setName(event.target.value)
              mutation.reset()
            }}
          />
          {invalidName || mutation.error ? (
            <FieldError id={`${id}-error`} role="alert">
              {invalidName
                ? t("files.invalidFolderName")
                : createdPath
                  ? t("files.folderCreatedRefreshFailed")
                  : mutation.error?.message}
            </FieldError>
          ) : null}
        </Field>
      </form>
      <DialogFooter className="flex-wrap">
        <Button
          type="button"
          variant="outline"
          className="min-h-11"
          disabled={mutation.isPending}
          onClick={onClose}
        >
          {t("settings.cancel")}
        </Button>
        <Button
          type="submit"
          form={id}
          aria-label={createdPath ? t("files.retry") : t("files.createFolder")}
          className="min-h-11"
          disabled={mutation.isPending || (!createdPath && (!trimmedName || invalidName))}
        >
          {mutation.isPending ? <Spinner data-icon="inline-start" /> : null}
          {createdPath ? t("files.retry") : t("files.createFolder")}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}
