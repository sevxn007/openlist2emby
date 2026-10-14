import { CSSProperties, ReactNode, useEffect, useState } from "react"
import {
  CalendarClock,
  FolderKanban,
  LibraryBig,
  Languages,
  type LucideIcon,
  LogOut,
  ServerCog,
  Settings,
  ShieldCheck,
  UsersRound,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Language } from "@/shared/i18n/messages"
import { useI18n } from "@/shared/i18n/useI18n"
import { ThemeMenu } from "@/shared/ui/ThemeMenu"
import { cn } from "@/lib/utils"

const SIDEBAR_COLLAPSED_KEY = "openlist2emby.sidebarCollapsed"

export type AppSection = "files" | "media" | "tasks" | "users" | "settings"

interface NavigationItem {
  key: AppSection
  label: string
  icon: LucideIcon
}

interface AppShellProps {
  activeSection: AppSection
  activeTaskCount: number
  children: ReactNode
  isAdmin: boolean
  mediaLibraryEnabled: boolean
  openListBaseURL?: string
  onNavigate: (section: AppSection) => void
  onLogout: () => void
}

export function AppShell({
  activeSection,
  activeTaskCount,
  children,
  isAdmin,
  mediaLibraryEnabled,
  openListBaseURL,
  onNavigate,
  onLogout,
}: AppShellProps) {
  const { language, setLanguage, t } = useI18n()
  const [sidebarOpen, setSidebarOpen] = useState(() => {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) !== "true"
  })

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(!sidebarOpen))
  }, [sidebarOpen])

  const navItems: NavigationItem[] = [
    ...(isAdmin
      ? [
          {
            key: "files" as const,
            label: t("app.nav.files"),
            icon: FolderKanban,
          },
        ]
      : []),
    ...(mediaLibraryEnabled
      ? [
          {
            key: "media" as const,
            label: t("app.nav.media"),
            icon: LibraryBig,
          },
        ]
      : []),
    ...(isAdmin
      ? [
          {
            key: "tasks" as const,
            label: t("app.nav.tasks"),
            icon: CalendarClock,
          },
          {
            key: "users" as const,
            label: t("app.nav.users"),
            icon: UsersRound,
          },
          {
            key: "settings" as const,
            label: t("app.settings"),
            icon: Settings,
          },
        ]
      : []),
  ]

  const compactPageTitle =
    activeSection === "settings"
      ? t("app.settings")
      : activeSection === "users"
        ? t("app.nav.users")
      : activeSection === "media"
        ? t("app.nav.media")
        : activeSection === "tasks"
          ? t("app.nav.tasks")
          : t("app.nav.files")
  const pageTitle =
    activeSection === "settings"
      ? t("settings.title")
      : activeSection === "users"
        ? t("app.page.users")
      : activeSection === "media"
        ? t("app.page.media")
        : activeSection === "tasks"
          ? t("app.page.tasks")
          : t("app.page.files")
  const mediaMode = activeSection === "media"

  return (
    <SidebarProvider
      open={sidebarOpen}
      onOpenChange={setSidebarOpen}
      style={
        {
          "--sidebar-width": "15rem",
          "--sidebar-width-icon": "4.5rem",
          "--app-sidebar-offset": sidebarOpen ? "15rem" : "4.5rem",
        } as CSSProperties
      }
    >
      <Sidebar collapsible="icon" className="border-r-0">
        <SidebarHeader className="px-3 pt-4 pb-2">
          <div className="flex h-12 items-center gap-3 overflow-hidden rounded-lg px-2">
            <div
              className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground shadow-sm"
            >
              <ServerCog className="size-5" />
            </div>
            <div className="min-w-0 truncate text-sm font-semibold text-sidebar-foreground group-data-[collapsible=icon]:hidden">
              {t("app.brand")}
            </div>
          </div>
        </SidebarHeader>

        <SidebarNavigation
          activeSection={activeSection}
          activeTaskCount={activeTaskCount}
          items={navItems}
          onNavigate={onNavigate}
        />

        <SidebarFooter className="items-center gap-2 p-3">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                aria-label={t("app.logout")}
                variant="ghost"
                size="icon"
                className="size-11 rounded-lg text-sidebar-foreground hover:bg-sidebar-accent"
                onClick={onLogout}
              >
                <LogOut />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">{t("app.logout")}</TooltipContent>
          </Tooltip>
          <SidebarTrigger
            aria-label={
              sidebarOpen ? t("app.sidebar.collapse") : t("app.sidebar.expand")
            }
            className="size-11 rounded-lg border border-sidebar-border bg-sidebar-accent text-sidebar-foreground hover:bg-sidebar-accent/80"
          />
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      <SidebarInset className="min-w-0 bg-background">
        <header
          className={cn(
            "sticky top-0 z-30 flex items-center gap-3 border-b px-4 backdrop-blur md:px-6",
            mediaMode
              ? "min-h-14 bg-card/95 md:hidden"
              : "min-h-16 bg-card/95",
          )}
        >
          <SidebarTrigger
            aria-label={
              sidebarOpen ? t("app.sidebar.collapse") : t("app.sidebar.expand")
            }
            className="md:hidden"
          />
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold md:text-lg">
              <span className="sm:hidden">{compactPageTitle}</span>
              <span className="hidden sm:inline">{pageTitle}</span>
            </h1>
            <div className={cn("mt-0.5 hidden min-w-0 items-center gap-2 sm:flex", mediaMode && "sm:hidden")}>
              <span className="truncate text-xs text-muted-foreground">
                {openListBaseURL || t("app.notConfigured")}
              </span>
              <Badge variant="outline" className="shrink-0 gap-1 text-primary">
                <ShieldCheck data-icon="inline-start" />
                {t("app.connected")}
              </Badge>
            </div>
          </div>

          <Select
            value={language}
            onValueChange={(value) => setLanguage(value as Language)}
          >
            <SelectTrigger
              aria-label={t("app.language")}
              className="w-11 justify-center px-0 sm:w-32 sm:justify-between sm:px-3 [&_[data-slot=select-value]]:hidden sm:[&_[data-slot=select-value]]:flex [&>svg:last-child]:hidden sm:[&>svg:last-child]:block"
            >
              <Languages data-icon="inline-start" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              <SelectGroup>
                <SelectItem value="en">{t("app.language.en")}</SelectItem>
                <SelectItem value="zh">{t("app.language.zh")}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>

          <ThemeMenu />

        </header>

        <div className={cn("flex flex-1 flex-col", mediaMode ? "p-0" : "p-4 md:p-6 lg:p-8")}>
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function SidebarNavigation({
  activeSection,
  activeTaskCount,
  items,
  onNavigate,
}: {
  activeSection: AppSection
  activeTaskCount: number
  items: NavigationItem[]
  onNavigate: (section: AppSection) => void
}) {
  const { setOpenMobile } = useSidebar()

  function handleNavigate(section: AppSection) {
    onNavigate(section)
    setOpenMobile(false)
  }

  return (
    <SidebarContent className="pt-3">
      <SidebarGroup className="px-3">
        <SidebarGroupContent>
          <SidebarMenu className="gap-2">
            {items.map((item) => {
              const Icon = item.icon
              return (
                <SidebarMenuItem key={item.key}>
                  <SidebarMenuButton
                    size="lg"
                    isActive={activeSection === item.key}
                    tooltip={item.label}
                    className="relative h-11 rounded-lg px-3 data-[active=true]:bg-sidebar-primary data-[active=true]:text-sidebar-primary-foreground group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
                    onClick={() => handleNavigate(item.key)}
                  >
                    <Icon />
                    <span className="group-data-[collapsible=icon]:hidden">
                      {item.label}
                    </span>
                    {item.key === "tasks" && activeTaskCount > 0 ? (
                      <Badge
                        className="ml-auto min-w-5 px-1 group-data-[collapsible=icon]:absolute group-data-[collapsible=icon]:-top-1 group-data-[collapsible=icon]:-right-1"
                        variant="destructive"
                      >
                        {activeTaskCount > 99 ? "99+" : activeTaskCount}
                      </Badge>
                    ) : null}
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )
            })}
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    </SidebarContent>
  )
}
