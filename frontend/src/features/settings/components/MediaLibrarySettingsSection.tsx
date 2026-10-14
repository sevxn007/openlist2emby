import { FormEvent, useState } from "react"
import { Check, Copy, HardDrive, LibraryBig, Save } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import {
  ManagerApiError,
  SettingsResponse,
  getTaskRun,
  migrateMediaMetadataStorage,
  updateSettings,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

interface MediaLibrarySettingsSectionProps {
  settings: SettingsResponse
  onSaved: (settings: SettingsResponse) => void
}

export function MediaLibrarySettingsSection({
  settings,
  onSaved,
}: MediaLibrarySettingsSectionProps) {
  const { t } = useI18n()
  const [enabled, setEnabled] = useState(settings.media_library_enabled)
  const [port, setPort] = useState(String(settings.emby_port || 8096))
  const [metadataPath, setMetadataPath] = useState(settings.media_metadata_path ?? "")
  const [error, setError] = useState("")
  const [isSaving, setIsSaving] = useState(false)
  const [copied, setCopied] = useState(false)
  const parsedPort = Number(port)
  const endpointURL = new URL(window.location.href)
  endpointURL.protocol = "http:"
  endpointURL.port = Number.isInteger(parsedPort) ? String(parsedPort) : ""
  endpointURL.pathname = "/"
  endpointURL.search = ""
  endpointURL.hash = ""
  const embyEndpoint = endpointURL.toString()

  async function copyEndpoint() {
    await navigator.clipboard.writeText(embyEndpoint)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1_500)
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    setIsSaving(true)
    try {
      const updatedSettings = await updateSettings({
        openlist_base_url: settings.openlist_base_url,
        openlist_admin_key: "",
        media_library_enabled: enabled,
        emby_port: parsedPort,
      })
      onSaved(updatedSettings)
      if (metadataPath.trim() !== (settings.media_metadata_path ?? "")) {
        const run = await migrateMediaMetadataStorage(metadataPath.trim())
        let current = run
        while (current.status === "queued" || current.status === "running") {
          await new Promise((resolve) => window.setTimeout(resolve, 1_000))
          current = await getTaskRun(run.id)
        }
        if (current.status !== "succeeded") {
          throw new ManagerApiError(
            current.error?.message || t("settings.mediaMetadataMigrationFailed"),
            409,
          )
        }
        const migrated = { ...updatedSettings, media_metadata_path: metadataPath.trim() }
        onSaved(migrated)
        toast.success(t("settings.mediaMetadataMigrated"))
      }
      toast.success(t("settings.mediaLibrarySaved"))
    } catch (err) {
      setError(
        err instanceof ManagerApiError
          ? err.message
          : t("settings.saveFailed"),
      )
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit}>
      <Card className="overflow-hidden shadow-sm">
        <CardHeader className="border-b px-5 py-5 md:px-6">
          <CardTitle className="flex items-center gap-2 text-base">
            <LibraryBig className="size-5 text-primary" />
            {t("settings.mediaLibraryTitle")}
          </CardTitle>
        </CardHeader>
        <CardContent className="px-5 md:px-6">
          <div className="flex min-h-14 items-center justify-between gap-4">
            <Label htmlFor="settings-media-library" className="text-sm">
              {t("settings.mediaLibraryEnabled")}
            </Label>
            <Switch
              id="settings-media-library"
              checked={enabled}
              disabled={isSaving}
              onCheckedChange={(checked) => {
                setEnabled(checked)
                setError("")
              }}
            />
          </div>
          {enabled ? (
            <div className="mt-4 flex flex-col gap-2 border-t pt-4">
              <Label htmlFor="settings-emby-port">
                {t("settings.embyPort")}
              </Label>
              <Input
                id="settings-emby-port"
                type="number"
                min={1}
                max={65535}
                value={port}
                disabled={isSaving}
                onChange={(event) => {
                  setPort(event.target.value)
                  setError("")
                }}
              />
              <Label htmlFor="settings-emby-endpoint">
                {t("settings.embyEndpoint")}
              </Label>
              <div className="flex gap-2">
                <Input
                  id="settings-emby-endpoint"
                  value={embyEndpoint}
                  readOnly
                  className="min-w-0 font-mono text-xs"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="icon-lg"
                  aria-label={t("settings.copyEmbyEndpoint")}
                  onClick={() => void copyEndpoint()}
                >
                  {copied ? <Check /> : <Copy />}
                </Button>
              </div>
              <div className="mt-2 flex items-center gap-2 border-t pt-4">
                <HardDrive className="size-4 text-muted-foreground" />
                <Label htmlFor="settings-media-metadata-path">
                  {t("settings.mediaMetadataPath")}
                </Label>
              </div>
              <Input
                id="settings-media-metadata-path"
                value={metadataPath}
                disabled={isSaving}
                className="font-mono text-xs"
                onChange={(event) => {
                  setMetadataPath(event.target.value)
                  setError("")
                }}
              />
              <p className="text-xs text-muted-foreground">
                {isSaving && metadataPath.trim() !== (settings.media_metadata_path ?? "")
                  ? t("settings.mediaMetadataMigrating")
                  : t("settings.mediaMetadataPathHint")}
              </p>
            </div>
          ) : null}
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
        </CardContent>
        <CardFooter className="justify-end">
          <Button
            type="submit"
            disabled={
              isSaving ||
              !Number.isInteger(parsedPort) ||
              parsedPort < 1 ||
              parsedPort > 65535
            }
          >
            {isSaving ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <Save data-icon="inline-start" />
            )}
            {t("settings.saveMediaLibrary")}
          </Button>
        </CardFooter>
      </Card>
    </form>
  )
}
