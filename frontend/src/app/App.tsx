import {lazy, Suspense, useEffect, useState} from "react"
import {useQuery, useQueryClient} from "@tanstack/react-query"

import {AppSection, AppShell} from "./AppShell"
import {LoginPage} from "../features/auth/LoginPage"
import {SetupPage} from "../features/auth/SetupPage"
import {taskQueryKeys} from "../features/tasks/queryKeys"
import {useTaskActivity} from "../features/tasks/useTaskActivity"
import {
  getCurrentUser,
  getStatus,
  logout,
  MANAGER_SESSION_EXPIRED_EVENT,
  SettingsResponse,
} from "../shared/api/manager"
import {useI18n} from "../shared/i18n/useI18n"
import {FullPageState} from "../shared/ui/FullPageState"
import {Spinner} from "../components/ui/spinner"

const FileBrowser = lazy(() =>
  import("../features/files/FileBrowser").then((module) => ({
    default: module.FileBrowser,
  })),
)
const SettingsPage = lazy(() =>
  import("../features/settings/SettingsPage").then((module) => ({
    default: module.SettingsPage,
  })),
)
const MediaLibraryPage = lazy(() =>
  import("../features/media/MediaLibraryPage").then((module) => ({
    default: module.MediaLibraryPage,
  })),
)
const TaskCenterPage = lazy(() =>
  import("../features/tasks/TaskCenterPage").then((module) => ({
    default: module.TaskCenterPage,
  })),
)
const UserManagementPage = lazy(() =>
  import("../features/users/UserManagementPage").then((module) => ({
    default: module.UserManagementPage,
  })),
)

const MediaPlaybackPage = lazy(
  () => import("../features/media/MediaPlaybackPage"),
)

const MEDIA_PLAYBACK_PATH = /^\/media\/play\/([1-9]\d*)\/?$/
const appSections: AppSection[] = ["files", "media", "tasks", "users", "settings"]

function mediaPlaybackItemID(pathname: string) {
  const match = MEDIA_PLAYBACK_PATH.exec(pathname)
  return match ? Number(match[1]) : null
}

function sectionFromLocation(): AppSection {
  const value = new URLSearchParams(window.location.search).get("section")
  return appSections.find((section) => section === value) ?? "files"
}

function setSectionInURL(section: AppSection, replace = false) {
  const url = new URL(window.location.href)
  url.searchParams.delete("path")
  if (section === "files") {
    url.searchParams.delete("section")
  } else {
    url.searchParams.set("section", section)
  }
  window.history[replace ? "replaceState" : "pushState"]({}, "", url)
}

export function App() {
  const queryClient = useQueryClient()
  const { t } = useI18n()
  const [activeSection, setActiveSection] = useState<AppSection>(sectionFromLocation)
  const [selectedTaskRunId, setSelectedTaskRunId] = useState<number | null>(
    null,
  )
  const [openListBaseURL, setOpenListBaseURL] = useState<string | undefined>()
  const [mediaLibraryEnabled, setMediaLibraryEnabled] = useState(false)
  const [pathname, setPathname] = useState(() => window.location.pathname)
  const [loggedOut, setLoggedOut] = useState(false)

  useEffect(() => {
    const handlePopState = () => {
      setPathname(window.location.pathname)
      if (mediaPlaybackItemID(window.location.pathname) === null) {
        setActiveSection(sectionFromLocation())
      }
    }
    window.addEventListener("popstate", handlePopState)
    return () => window.removeEventListener("popstate", handlePopState)
  }, [])

  useEffect(() => {
    const handleSessionExpired = () => {
      setLoggedOut(true)
      queryClient.removeQueries({ queryKey: ["me"] })
      queryClient.removeQueries({ queryKey: taskQueryKeys.all })
    }
    window.addEventListener(MANAGER_SESSION_EXPIRED_EVENT, handleSessionExpired)
    return () =>
      window.removeEventListener(
        MANAGER_SESSION_EXPIRED_EVENT,
        handleSessionExpired,
      )
  }, [queryClient])

  const statusQuery = useQuery({
    queryKey: ["status"],
    queryFn: getStatus,
  })

  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: getCurrentUser,
    enabled: statusQuery.isSuccess && !statusQuery.data.setup_required,
    retry: false,
  })

  const isAdmin =
    meQuery.data?.is_admin === true || meQuery.data?.role === "admin"
  const taskActivity = useTaskActivity(isAdmin)

  useEffect(() => {
    if (statusQuery.data?.openlist_base_url) {
      setOpenListBaseURL(statusQuery.data.openlist_base_url)
    }
  }, [statusQuery.data?.openlist_base_url])

  useEffect(() => {
    setMediaLibraryEnabled(statusQuery.data?.media_library_enabled === true)
  }, [statusQuery.data?.media_library_enabled])

  function refreshStatus() {
    void queryClient.invalidateQueries({ queryKey: ["status"] })
    void queryClient.invalidateQueries({ queryKey: ["me"] })
  }

  async function handleLogout() {
    await logout()
    setLoggedOut(true)
    queryClient.removeQueries({ queryKey: ["me"] })
    void queryClient.removeQueries({ queryKey: taskQueryKeys.all })
    refreshStatus()
  }

  function handleLoginComplete() {
    setLoggedOut(false)
    refreshStatus()
  }

  function handleSettingsSaved(settings: SettingsResponse) {
    setOpenListBaseURL(settings.openlist_base_url)
    setMediaLibraryEnabled(settings.media_library_enabled)
    if (!settings.media_library_enabled && activeSection === "media") {
      setActiveSection("files")
      setSectionInURL("files", true)
    }
    void queryClient.invalidateQueries({ queryKey: ["status"] })
  }

  function handleNavigate(section: AppSection) {
    if (!isAdmin && section !== "media") {
      return
    }
    if (isAdmin && section === "media" && !mediaLibraryEnabled) {
      setActiveSection("files")
      setSectionInURL("files")
      return
    }
    setSelectedTaskRunId(null)
    setActiveSection(section)
    setSectionInURL(section)
  }

  function handleViewTask(runId: number) {
    if (!isAdmin) {
      return
    }
    setSelectedTaskRunId(runId)
    setActiveSection("tasks")
    setSectionInURL("tasks")
  }

  function handlePlayMedia(itemID: number) {
    const nextPath = `/media/play/${itemID}`
    window.history.pushState({ openListMediaPlayback: true }, "", nextPath)
    setPathname(nextPath)
  }

  function handleLeavePlayback() {
    const state = window.history.state as
      | { openListMediaPlayback?: boolean }
      | null
    if (state?.openListMediaPlayback) {
      window.history.back()
      return
    }
    window.history.replaceState(null, "", "/?section=media")
    setPathname("/")
    setActiveSection(!isAdmin || mediaLibraryEnabled ? "media" : "files")
  }

  if (statusQuery.isLoading) {
    return (
      <FullPageState
        loading
        title={t("state.loadingManager")}
        description={t("state.checkingSetup")}
      />
    )
  }

  if (statusQuery.isError) {
    return (
      <FullPageState
        title={t("state.managerUnavailable")}
        description={(statusQuery.error as Error).message}
        onRetry={() => void statusQuery.refetch()}
      />
    )
  }

  const status = statusQuery.data

  if (!status) {
    return (
      <FullPageState
        title={t("state.managerUnavailable")}
        description={t("state.noStatus")}
      />
    )
  }

  if (status.setup_required) {
    return <SetupPage onSetupComplete={refreshStatus} />
  }

  if (loggedOut) {
    return (
      <LoginPage
        openListBaseURL={status.openlist_base_url}
        onLoginComplete={handleLoginComplete}
      />
    )
  }

  if (meQuery.isLoading) {
    return (
      <FullPageState
        loading
        title={t("state.loadingSession")}
        description={t("state.checkingSession")}
      />
    )
  }

  if (meQuery.isError || !meQuery.data) {
    return (
      <LoginPage
        openListBaseURL={status.openlist_base_url}
        onLoginComplete={refreshStatus}
      />
    )
  }

  const resolvedBaseURL = openListBaseURL || status.openlist_base_url
  const playbackItemID = mediaPlaybackItemID(pathname)
  const visibleSection: AppSection = !isAdmin
    ? "media"
    : activeSection === "media" && !mediaLibraryEnabled
      ? "files"
      : activeSection

  if (playbackItemID !== null) {
    return (
      <Suspense
        fallback={
          <FullPageState
            loading
            title={t("playback.loading")}
            description={t("playback.preparing")}
          />
        }
      >
        <MediaPlaybackPage
          itemID={playbackItemID}
          onBack={handleLeavePlayback}
          onPlayItem={handlePlayMedia}
        />
      </Suspense>
    )
  }

  const activePage = !isAdmin ? (
    mediaLibraryEnabled ? (
      <MediaLibraryPage
        isAdmin={false}
        onTaskCreated={taskActivity.trackTaskRun}
        onViewTask={handleViewTask}
        onPlay={(item) => handlePlayMedia(item.id)}
      />
    ) : (
      <FullPageState
        title={t("mediaLibrary.unavailable")}
        description={t("mediaLibrary.unavailableDescription")}
      />
    )
    ) : visibleSection === "media" && mediaLibraryEnabled ? (
      <MediaLibraryPage
        isAdmin
        onTaskCreated={taskActivity.trackTaskRun}
        onViewTask={handleViewTask}
        onPlay={(item) => handlePlayMedia(item.id)}
      />
    ) : visibleSection === "tasks" ? (
      <TaskCenterPage
        selectedRunId={selectedTaskRunId}
        onSelectedRunChange={setSelectedTaskRunId}
        onTaskCreated={taskActivity.trackTaskRun}
      />
    ) : visibleSection === "users" ? (
      <UserManagementPage currentUserID={meQuery.data.id} />
    ) : visibleSection === "settings" ? (
      <SettingsPage onSaved={handleSettingsSaved} />
    ) : (
      <FileBrowser
        openListBaseURL={resolvedBaseURL}
      />
    )

  return (
    <AppShell
      activeSection={visibleSection}
      activeTaskCount={taskActivity.activeTaskCount}
      isAdmin={isAdmin}
      mediaLibraryEnabled={mediaLibraryEnabled}
      openListBaseURL={resolvedBaseURL}
      onLogout={() => void handleLogout()}
      onNavigate={handleNavigate}
    >
      <Suspense
        fallback={
          <div className="grid min-h-[50vh] place-items-center" aria-live="polite">
            <Spinner />
          </div>
        }
      >
        {activePage}
      </Suspense>
    </AppShell>
  )
}
