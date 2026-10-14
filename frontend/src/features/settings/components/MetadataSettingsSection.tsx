import {FormEvent, useState} from "react"
import {useMutation} from "@tanstack/react-query"
import {Film, PlugZap, Save} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Card, CardContent, CardFooter, CardHeader, CardTitle} from "@/components/ui/card"
import {Field, FieldError, FieldGroup, FieldLabel} from "@/components/ui/field"
import {Input} from "@/components/ui/input"
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from "@/components/ui/select"
import {Spinner} from "@/components/ui/spinner"
import {
  ManagerApiError,
  MetadataSource,
  SettingsResponse,
  testTMDBConnection,
  updateSettings,
} from "@/shared/api/manager"
import {useI18n} from "@/shared/i18n/useI18n"

interface MetadataSettingsSectionProps {
  settings: SettingsResponse
  onSaved: (settings: SettingsResponse) => void
}

export function MetadataSettingsSection({ settings, onSaved }: MetadataSettingsSectionProps) {
  const { t } = useI18n()
  const [tmdbKey, setTMDBKey] = useState("")
  const [tvdbKey, setTVDBKey] = useState("")
  const [pin, setPIN] = useState("")
  const [priority, setPriority] = useState((settings.metadata_priority ?? ["tmdb", "tvdb"]).join(","))
  const [error, setError] = useState("")

  const save = useMutation({
    mutationFn: () => updateSettings({
      openlist_base_url: settings.openlist_base_url,
      openlist_admin_key: "",
      tmdb_api_key: tmdbKey.trim(),
      tvdb_api_key: tvdbKey.trim(),
      tvdb_pin: pin.trim(),
      metadata_priority: priority === "tvdb,tmdb" ? ["tvdb", "tmdb"] : ["tmdb", "tvdb"],
    }),
    onSuccess: (updated) => {
      setTMDBKey("")
      setTVDBKey("")
      setPIN("")
      onSaved(updated)
      toast.success(t("settings.metadataSaved"))
    },
    onError: (err) => setError(err instanceof ManagerApiError ? err.message : t("settings.saveFailed")),
  })
  const test = useMutation({
    mutationFn: (source: MetadataSource) => testTMDBConnection({
      source,
      tmdb_api_key: tmdbKey.trim(),
      tvdb_api_key: tvdbKey.trim(),
      tvdb_pin: pin.trim(),
    }),
    onSuccess: () => toast.success(t("settings.metadataConnected")),
    onError: (err) => setError(err instanceof ManagerApiError ? err.message : t("settings.metadataConnectionFailed")),
  })
  const busy = save.isPending || test.isPending

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    save.mutate()
  }

  return (
    <form className="@container/metadata-settings" onSubmit={submit} aria-labelledby="settings-metadata-title">
      <Card className="overflow-hidden shadow-sm">
        <CardHeader className="border-b px-5 py-5 md:px-6">
          <CardTitle id="settings-metadata-title" className="flex items-center gap-2 text-base">
            <Film aria-hidden="true" className="size-5 text-primary" />
            {t("settings.metadataTitle")}
          </CardTitle>
        </CardHeader>
        <CardContent className="min-w-0 px-5 md:px-6">
          <FieldGroup>
            <div className="grid min-w-0 gap-5 @xl/field-group:grid-cols-2">
              {(["tmdb", "tvdb"] as const).map((source) => {
                const testing = test.isPending && test.variables === source
                return (
                  <section
                    key={source}
                    aria-labelledby={`settings-${source}-title`}
                    className="flex min-w-0 flex-col overflow-hidden rounded-lg border border-border bg-background"
                  >
                    <h3 id={`settings-${source}-title`} className="border-b px-4 py-3 text-sm font-medium">
                      {source.toUpperCase()}
                    </h3>
                    <FieldGroup className="flex-1 p-4">
                      <Field>
                        <FieldLabel htmlFor={`settings-${source}-key`}>
                          {t(source === "tmdb" ? "settings.tmdbApiKey" : "settings.tvdbApiKey")}
                        </FieldLabel>
                        <Input
                          id={`settings-${source}-key`}
                          type="password"
                          autoComplete="new-password"
                          spellCheck={false}
                          value={source === "tmdb" ? tmdbKey : tvdbKey}
                          onChange={(event) => {
                            if (source === "tmdb") setTMDBKey(event.target.value)
                            else setTVDBKey(event.target.value)
                            setError("")
                          }}
                          disabled={busy}
                          placeholder={(source === "tmdb" ? settings.tmdb_api_key_set : settings.tvdb_api_key_set) ? t("settings.keepKey") : ""}
                        />
                      </Field>
                      {source === "tvdb" ? (
                        <Field>
                          <FieldLabel htmlFor="settings-tvdb-pin">{t("settings.tvdbPIN")}</FieldLabel>
                          <Input
                            id="settings-tvdb-pin"
                            type="password"
                            autoComplete="new-password"
                            spellCheck={false}
                            value={pin}
                            onChange={(event) => {
                              setPIN(event.target.value)
                              setError("")
                            }}
                            disabled={busy}
                            placeholder={settings.tvdb_pin_set ? t("settings.keepKey") : ""}
                          />
                        </Field>
                      ) : null}
                    </FieldGroup>
                    <div className="border-t bg-muted/50 p-4">
                      <Button
                        type="button"
                        variant="outline"
                        className="w-full"
                        disabled={busy}
                        onClick={() => {
                          setError("")
                          test.mutate(source)
                        }}
                      >
                        {testing ? <Spinner aria-hidden="true" data-icon="inline-start" /> : <PlugZap aria-hidden="true" data-icon="inline-start" />}
                        {t(testing ? "settings.testingConnection" : source === "tmdb" ? "settings.testTMDB" : "settings.testTVDB")}
                      </Button>
                    </div>
                  </section>
                )
              })}
            </div>
            <Field>
              <FieldLabel htmlFor="settings-metadata-priority">{t("settings.metadataPriority")}</FieldLabel>
              <Select
                value={priority}
                onValueChange={(value) => { setPriority(value); setError("") }}
                disabled={busy}
              >
                <SelectTrigger id="settings-metadata-priority" className="w-full min-w-0"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="tmdb,tvdb">{t("settings.priorityTMDBFirst")}</SelectItem>
                  <SelectItem value="tvdb,tmdb">{t("settings.priorityTVDBFirst")}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            {error ? <FieldError role="alert" className="break-words">{error}</FieldError> : null}
          </FieldGroup>
        </CardContent>
          <CardFooter
              className="flex-col items-stretch gap-3 px-5 md:px-6 @xl/metadata-settings:flex-row @xl/metadata-settings:items-center @xl/metadata-settings:justify-end">
          <Button type="submit" disabled={busy}>
            {save.isPending ? <Spinner aria-hidden="true" data-icon="inline-start" /> : <Save aria-hidden="true" data-icon="inline-start" />}
            {t("settings.saveMetadata")}
          </Button>
        </CardFooter>
      </Card>
    </form>
  )
}
