import {FormEvent, useEffect, useMemo, useState} from "react"
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query"
import {CircleAlert, Pencil, Plus, UsersRound,} from "lucide-react"
import {toast} from "sonner"

import {Alert, AlertAction, AlertDescription, AlertTitle,} from "@/components/ui/alert"
import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle,} from "@/components/ui/card"
import {Checkbox} from "@/components/ui/checkbox"
import {Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,} from "@/components/ui/dialog"
import {Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle,} from "@/components/ui/empty"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import {Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue,} from "@/components/ui/select"
import {Skeleton} from "@/components/ui/skeleton"
import {Spinner} from "@/components/ui/spinner"
import {Switch} from "@/components/ui/switch"
import {
  grantIdentity,
  listAllMediaLibraries,
  listManagerUsers,
  listOpenListUsers,
  LocalUserRole,
  ManagerApiError,
  ManagerUser,
  MediaLibraryGrant,
  updateManagerUser,
} from "@/shared/api/manager"
import {useI18n} from "@/shared/i18n/useI18n"

interface UserManagementPageProps {
  currentUserID: number
}

interface UserEditorDialogProps {
  currentUserID: number
  open: boolean
  user: ManagerUser | null
  onClose: () => void
  onSaved: () => void
}

export function UserManagementPage({ currentUserID }: UserManagementPageProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [editorUser, setEditorUser] = useState<ManagerUser | null | undefined>(
    undefined,
  )

  const usersQuery = useQuery({
    queryKey: ["manager-users"],
    queryFn: listManagerUsers,
  })

  const statusMutation = useMutation({
    mutationFn: ({ user, enabled }: { user: ManagerUser; enabled: boolean }) =>
      updateManagerUser(user.id, { enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["manager-users"] })
      toast.success(t("users.saved"))
    },
    onError: (error) => toast.error(userErrorMessage(error, t("users.saveFailed"))),
  })

  if (usersQuery.isLoading) {
    return <UserManagementSkeleton />
  }

  if (usersQuery.isError || !usersQuery.data) {
    return (
      <Alert variant="destructive" className="w-full max-w-5xl py-3">
        <CircleAlert />
        <AlertTitle>{t("users.loadFailed")}</AlertTitle>
        <AlertDescription>
          {userErrorMessage(usersQuery.error, t("users.loadFailed"))}
        </AlertDescription>
        <AlertAction>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void usersQuery.refetch()}
          >
            {t("state.retry")}
          </Button>
        </AlertAction>
      </Alert>
    )
  }

  const users = usersQuery.data.items

  return (
    <div className="flex w-full max-w-5xl flex-col gap-6">
      <Card>
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            <UsersRound data-icon="inline-start" />
            {t("users.title")}
          </CardTitle>
          <CardDescription>{t("users.description")}</CardDescription>
          <CardAction>
            <Button type="button" onClick={() => setEditorUser(null)}>
              <Plus data-icon="inline-start" />
              {t("users.add")}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          {users.length === 0 ? (
            <Empty className="min-h-72 rounded-none border-x-0 border-b-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <UsersRound />
                </EmptyMedia>
                <EmptyTitle>{t("users.empty")}</EmptyTitle>
                <EmptyDescription>{t("users.emptyDescription")}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button type="button" onClick={() => setEditorUser(null)}>
                  <Plus data-icon="inline-start" />
                  {t("users.add")}
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <UserTable
              currentUserID={currentUserID}
              isChangingStatus={statusMutation.isPending}
              users={users}
              onEdit={setEditorUser}
              onEnabledChange={(user, enabled) =>
                statusMutation.mutate({ user, enabled })
              }
            />
          )}
        </CardContent>
      </Card>

      <UserEditorDialog
        currentUserID={currentUserID}
        open={editorUser !== undefined}
        user={editorUser ?? null}
        onClose={() => setEditorUser(undefined)}
        onSaved={() => {
          setEditorUser(undefined)
          void queryClient.invalidateQueries({ queryKey: ["manager-users"] })
          void queryClient.invalidateQueries({ queryKey: ["me"] })
        }}
      />


    </div>
  )
}

function UserTable({
  currentUserID,
  isChangingStatus,
  users,
  onEdit,
  onEnabledChange,
}: {
  currentUserID: number
  isChangingStatus: boolean
  users: ManagerUser[]
  onEdit: (user: ManagerUser) => void
  onEnabledChange: (user: ManagerUser, enabled: boolean) => void
}) {
  const { t } = useI18n()

  return (
    <div className="divide-y">
      {users.map((user) => {
        const isCurrentUser = user.id === currentUserID
        return (
          <div
            key={user.id}
            className="flex min-w-0 flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center sm:px-6"
          >
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <span className="truncate font-medium">{user.username}</span>
                {isCurrentUser ? (
                  <Badge variant="outline">{t("users.current")}</Badge>
                ) : null}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                <Badge variant={user.role === "admin" ? "default" : "secondary"}>
                  {roleLabel(user.role, t)}
                </Badge>
                <span>{libraryGrantLabel(user, t)}</span>
                <span>{t(`users.upstream.${user.upstream_status}`)}</span>
              </div>
            </div>

            <div className="flex items-center justify-between gap-2 sm:justify-end">
              <Field orientation="horizontal" className="w-auto items-center gap-2">
                <FieldLabel htmlFor={`user-enabled-${user.id}`} className="text-sm">
                  {user.enabled ? t("users.enabled") : t("users.disabled")}
                </FieldLabel>
                <Switch
                  id={`user-enabled-${user.id}`}
                  checked={user.enabled}
                  disabled={isChangingStatus || isCurrentUser}
                  aria-label={t("users.toggleStatus", { username: user.username })}
                  onCheckedChange={(enabled) => onEnabledChange(user, enabled)}
                />
              </Field>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={t("users.editUser", { username: user.username })}
                onClick={() => onEdit(user)}
              >
                <Pencil />
              </Button>

            </div>
          </div>
        )
      })}
    </div>
  )
}

function UserEditorDialog({
  currentUserID,
  open,
  user,
  onClose,
  onSaved,
}: UserEditorDialogProps) {
  const { t } = useI18n()
  const [identityID, setIdentityID] = useState("")
  const [identityPage, setIdentityPage] = useState(1)
  const identitiesQuery = useQuery({
    queryKey: ["openlist-users", identityPage],
    queryFn: () => listOpenListUsers(identityPage),
    enabled: open && !user,
  })
  const [role, setRole] = useState<LocalUserRole>("media")
  const [enabled, setEnabled] = useState(true)
  const [libraryGrants, setLibraryGrants] = useState<MediaLibraryGrant[]>([])
  const [error, setError] = useState("")

  const librariesQuery = useQuery({
    queryKey: ["media-libraries", "all"],
    queryFn: listAllMediaLibraries,
    enabled: open,
  })
  const libraries = librariesQuery.data?.items ?? []
  const grantsByLibraryID = useMemo(
    () => new Map(libraryGrants.map((grant) => [grant.library_id, grant])),
    [libraryGrants],
  )
  const isCurrentUser = user?.id === currentUserID

  useEffect(() => {
    if (!open) {
      return
    }
    setIdentityID("")
    setIdentityPage(1)
    setRole(user?.role ?? "media")
    setEnabled(user?.enabled ?? true)
    setLibraryGrants(user?.library_grants ?? [])
    setError("")
  }, [open, user])

  const saveMutation = useMutation({
    mutationFn: () => {
      const payload = {
        role,
        enabled,
          library_grants: libraryGrants.map((grant) => ({...grant, access: "view" as const})),
      }
      if (user) {
        return updateManagerUser(user.id, payload)
      }
      return grantIdentity({ ...payload, openlist_user_id: Number(identityID) })
    },
    onSuccess: () => {
      toast.success(t("users.saved"))
      onSaved()
    },
    onError: (mutationError) =>
      setError(userErrorMessage(mutationError, t("users.saveFailed"))),
  })

  function toggleLibrary(libraryID: number, checked: boolean | "indeterminate") {
    setLibraryGrants((current) => {
      if (checked === true) {
        return current.some((grant) => grant.library_id === libraryID)
          ? current
          : [...current, { library_id: libraryID, access: "view" }]
      }
      return current.filter((grant) => grant.library_id !== libraryID)
    })
    setError("")
  }

  function changeIdentityPage(page: number) {
    setIdentityID("")
    setIdentityPage(page)
    setError("")
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!user && !identityID) {
      setError(t("users.selectOpenList"))
      return
    }
    if (role === "media" && libraryGrants.length === 0) {
      setError(t("users.libraryRequired"))
      return
    }
    setError("")
    saveMutation.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="grid max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-lg">
        <form className="contents" onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{user ? t("users.edit") : t("users.add")}</DialogTitle>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-5 pr-1">
            <FieldGroup>
              {user ? (
                <p className="break-all text-sm">{user.username}</p>
              ) : (
                <Field>
                  <FieldLabel htmlFor="openlist-user">{t("users.selectOpenList")}</FieldLabel>
                  <Select value={identityID} onValueChange={setIdentityID} disabled={identitiesQuery.isFetching}>
                    <SelectTrigger id="openlist-user" className="min-h-11 w-full min-w-0">
                      <SelectValue placeholder={t("users.selectOpenList")} />
                    </SelectTrigger>
                    <SelectContent>
                      {(identitiesQuery.data?.content ?? [])
                        .filter((identity) => identity.role !== 1)
                        .map((identity) => (
                          <SelectItem key={identity.id} value={String(identity.id)} disabled={identity.disabled}>
                            <span className="break-all">
                              {identity.username}{identity.disabled ? ` (${t("users.upstream.disabled")})` : ""}
                            </span>
                          </SelectItem>
                        ))}
                    </SelectContent>
                  </Select>
                  {identitiesQuery.isLoading ? <Spinner /> : null}
                  {identitiesQuery.isError ? <FieldError>{t("users.loadFailed")}</FieldError> : null}
                  <div className="flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      className="min-h-11"
                      disabled={identityPage <= 1 || identitiesQuery.isFetching}
                      onClick={() => changeIdentityPage(identityPage - 1)}
                    >
                      {t("users.previousPage")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="min-h-11"
                      disabled={identitiesQuery.isFetching || !identitiesQuery.data || identityPage * 50 >= identitiesQuery.data.total}
                      onClick={() => changeIdentityPage(identityPage + 1)}
                    >
                      {t("users.nextPage")}
                    </Button>
                    {identitiesQuery.isError ? (
                      <Button type="button" variant="outline" onClick={() => void identitiesQuery.refetch()}>
                        {t("state.retry")}
                      </Button>
                    ) : null}
                  </div>
                </Field>
              )}

              <Field>
                <FieldLabel htmlFor="manager-user-role">{t("users.role")}</FieldLabel>
                <Select
                  value={role}
                  onValueChange={(value) => {
                    setRole(value === "admin" ? "admin" : "media")
                    setError("")
                  }}
                >
                  <SelectTrigger id="manager-user-role" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="media">{t("users.roleMedia")}</SelectItem>
                      <SelectItem value="admin">{t("users.roleAdmin")}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {role === "admin"
                    ? t("users.roleAdminDescription")
                    : t("users.roleMediaDescription")}
                </FieldDescription>
              </Field>

              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel htmlFor="manager-user-enabled">
                    {t("users.status")}
                  </FieldLabel>
                  <FieldDescription>
                    {isCurrentUser
                      ? t("users.currentUserStatusHint")
                      : t("users.statusHint")}
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="manager-user-enabled"
                  checked={enabled}
                  disabled={isCurrentUser}
                  aria-label={t("users.status")}
                  onCheckedChange={(next) => {
                    setEnabled(next)
                    setError("")
                  }}
                />
              </Field>

              <FieldSet data-invalid={Boolean(error)}>
                  <FieldLegend variant="label">{t("users.libraries")}</FieldLegend>
                  <FieldDescription>
                    {role === "admin"
                      ? t("users.adminLibrariesHint")
                      : t("users.librariesHint")}
                  </FieldDescription>
                  {librariesQuery.isLoading ? (
                    <div className="flex min-h-11 items-center gap-2 text-sm text-muted-foreground">
                      <Spinner />
                      {t("common.loading")}
                    </div>
                  ) : librariesQuery.isError ? (
                    <FieldError>{t("users.librariesLoadFailed")}</FieldError>
                  ) : libraries.length === 0 ? (
                    <FieldError>
                      {role === "admin"
                        ? t("users.noLibrariesAdmin")
                        : t("users.noLibraries")}
                    </FieldError>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {libraries.map((library) => (
                        <Field
                          key={library.id}
                          orientation="horizontal"
                          data-disabled={!grantsByLibraryID.has(library.id)}
                        >
                          <Checkbox
                            id={`manager-user-library-${library.id}`}
                            checked={grantsByLibraryID.has(library.id)}
                            onCheckedChange={(checked) =>
                              toggleLibrary(library.id, checked)
                            }
                          />
                          <FieldContent>
                            <FieldLabel htmlFor={`manager-user-library-${library.id}`}>
                              {library.name}
                            </FieldLabel>
                            <FieldDescription>
                              {t(`mediaLibrary.type.${library.content_type}`)}
                            </FieldDescription>
                          </FieldContent>
                        </Field>
                      ))}
                    </div>
                  )}
                </FieldSet>

              {error ? <FieldError>{error}</FieldError> : null}
            </FieldGroup>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t("settings.cancel")}
            </Button>
            <Button type="submit" disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Spinner data-icon="inline-start" /> : null}
              {t("users.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function UserManagementSkeleton() {
  return (
    <Card className="w-full max-w-5xl">
      <CardHeader className="border-b">
        <Skeleton className="h-5 w-36" />
        <Skeleton className="h-4 w-72" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4 py-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton className="h-16 w-full" key={index} />
        ))}
      </CardContent>
    </Card>
  )
}

function roleLabel(role: LocalUserRole, t: ReturnType<typeof useI18n>["t"]) {
  return role === "admin" ? t("users.roleAdmin") : t("users.roleMedia")
}

function libraryGrantLabel(user: ManagerUser, t: ReturnType<typeof useI18n>["t"]) {
  return t("users.libraryCount", { count: user.library_grants.length })
}

function userErrorMessage(error: unknown, fallback: string) {
  return error instanceof ManagerApiError && error.message ? error.message : fallback
}
