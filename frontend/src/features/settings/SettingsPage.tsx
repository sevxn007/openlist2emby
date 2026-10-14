import { CircleAlert } from "lucide-react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardHeader,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { getSettings, SettingsResponse } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

import { OpenListSettingsSection } from "./components/OpenListSettingsSection"
import { MediaLibrarySettingsSection } from "./components/MediaLibrarySettingsSection"
import { MetadataSettingsSection } from "./components/MetadataSettingsSection"

interface SettingsPageProps {
  onSaved: (settings: SettingsResponse) => void
}

export function SettingsPage({ onSaved }: SettingsPageProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const settingsQuery = useQuery({
    queryKey: ["settings"],
    queryFn: getSettings,
  })

  function handleSaved(settings: SettingsResponse) {
    queryClient.setQueryData(["settings"], settings)
    onSaved(settings)
  }

  if (settingsQuery.isLoading) {
    return (
      <div
        className="flex w-full max-w-3xl flex-col gap-6"
        aria-label={t("state.loadingManager")}
      >
        {[2, 1].map((fieldCount) => (
          <Card key={fieldCount} className="shadow-sm">
            <CardHeader className="border-b px-5 py-5 md:px-6">
              <Skeleton className="h-5 w-32" />
            </CardHeader>
            <CardContent className="flex flex-col gap-5 px-5 md:px-6">
              {Array.from({ length: fieldCount }).map((_, index) => (
                <Skeleton className="h-17 w-full" key={index} />
              ))}
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  if (settingsQuery.isError || !settingsQuery.data) {
    return (
      <Alert variant="destructive" className="w-full max-w-3xl py-3">
        <CircleAlert />
        <AlertTitle>{t("settings.loadFailed")}</AlertTitle>
        <AlertDescription>
          {(settingsQuery.error as Error | null)?.message || t("settings.loadFailed")}
        </AlertDescription>
        <AlertAction>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void settingsQuery.refetch()}
          >
            {t("state.retry")}
          </Button>
        </AlertAction>
      </Alert>
    )
  }

  return (
    <div className="flex w-full max-w-3xl flex-col gap-6">
      <OpenListSettingsSection
        settings={settingsQuery.data}
        onSaved={handleSaved}
      />
      <MetadataSettingsSection
        settings={settingsQuery.data}
        onSaved={handleSaved}
      />
      <MediaLibrarySettingsSection
        settings={settingsQuery.data}
        onSaved={handleSaved}
      />
    </div>
  )
}
