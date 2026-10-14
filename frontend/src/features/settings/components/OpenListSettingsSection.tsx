import { FormEvent, useRef, useState } from "react"
import { PlugZap, Save, ServerCog } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import {
  ManagerApiError,
  SettingsResponse,
  testSettingsConnection,
  updateSettings,
} from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"

interface OpenListSettingsSectionProps {
  settings: SettingsResponse
  onSaved: (settings: SettingsResponse) => void
}

export function OpenListSettingsSection({
  settings,
  onSaved,
}: OpenListSettingsSectionProps) {
  const { t } = useI18n()
  const [baseURL, setBaseURL] = useState(settings.openlist_base_url)
  const [adminKey, setAdminKey] = useState("")
  const [error, setError] = useState("")
  const [isSaving, setIsSaving] = useState(false)
  const [isTesting, setIsTesting] = useState(false)
  const formRef = useRef<HTMLFormElement>(null)
  const isBusy = isSaving || isTesting

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    setIsSaving(true)
    try {
      const updatedSettings = await updateSettings({
        openlist_base_url: baseURL,
        openlist_admin_key: adminKey,
      })
      setBaseURL(updatedSettings.openlist_base_url)
      setAdminKey("")
      onSaved(updatedSettings)
      toast.success(t("settings.openListSaved"))
    } catch (err) {
      setError(err instanceof ManagerApiError ? err.message : t("settings.saveFailed"))
    } finally {
      setIsSaving(false)
    }
  }

  async function handleTestConnection() {
    if (!formRef.current?.reportValidity()) {
      return
    }
    setError("")
    setIsTesting(true)
    try {
      await testSettingsConnection({
        openlist_base_url: baseURL,
        openlist_admin_key: adminKey,
      })
      toast.success(t("settings.connectionSuccess"))
    } catch (err) {
      const message =
        err instanceof ManagerApiError ? err.message : t("settings.connectionFailed")
      setError(message)
      toast.error(message)
    } finally {
      setIsTesting(false)
    }
  }

  return (
    <form ref={formRef} onSubmit={handleSubmit}>
      <Card className="overflow-hidden shadow-sm">
        <CardHeader className="border-b px-5 py-5 md:px-6">
          <CardTitle className="flex items-center gap-2 text-base">
            <ServerCog className="size-5 text-primary" />
            {t("settings.openListTitle")}
          </CardTitle>
        </CardHeader>

        <CardContent className="px-5 md:px-6">
          <FieldGroup>
            <Field data-invalid={Boolean(error)}>
              <FieldLabel htmlFor="settings-openlist-url">
                {t("settings.openListAddress")}
              </FieldLabel>
              <Input
                id="settings-openlist-url"
                value={baseURL}
                onChange={(event) => {
                  setBaseURL(event.target.value)
                  setError("")
                }}
                type="url"
                aria-invalid={Boolean(error)}
                required
              />
            </Field>

            <Field data-invalid={Boolean(error)}>
              <FieldLabel htmlFor="settings-admin-key">
                {t("settings.adminKey")}
              </FieldLabel>
              <Input
                id="settings-admin-key"
                value={adminKey}
                onChange={(event) => {
                  setAdminKey(event.target.value)
                  setError("")
                }}
                placeholder={
                  settings.openlist_admin_key_set
                    ? t("settings.keepKey")
                    : t("settings.tokenPlaceholder")
                }
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                aria-invalid={Boolean(error)}
              />
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
          </FieldGroup>
        </CardContent>

        <CardFooter className="flex-wrap justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={isBusy}
            onClick={() => void handleTestConnection()}
          >
            {isTesting ? (
              <Spinner
                data-icon="inline-start"
                aria-label={t("settings.testingConnection")}
              />
            ) : (
              <PlugZap data-icon="inline-start" />
            )}
            {isTesting ? t("settings.testingConnection") : t("settings.testConnection")}
          </Button>
          <Button type="submit" disabled={isBusy}>
            {isSaving ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <Save data-icon="inline-start" />
            )}
            {t("settings.saveOpenList")}
          </Button>
        </CardFooter>
      </Card>
    </form>
  )
}
