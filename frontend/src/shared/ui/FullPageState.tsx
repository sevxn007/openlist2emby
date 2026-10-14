import { RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Spinner } from "@/components/ui/spinner"
import { useI18n } from "@/shared/i18n/useI18n"

interface FullPageStateProps {
  title: string
  description?: string
  loading?: boolean
  onRetry?: () => void
}

export function FullPageState({
  title,
  description,
  loading = false,
  onRetry,
}: FullPageStateProps) {
  const { t } = useI18n()

  return (
    <main className="grid min-h-svh place-items-center bg-background p-4" aria-live={loading ? "polite" : undefined}>
      <Card className="w-full max-w-sm shadow-sm">
        <CardHeader className="items-center text-center">
          {loading ? <Spinner className="mb-2 size-5" /> : null}
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        {description || onRetry ? (
          <CardContent className="flex flex-col items-center gap-4 text-center">
            {description ? (
              <p className="text-sm text-muted-foreground">{description}</p>
            ) : null}
            {onRetry ? (
              <Button variant="outline" onClick={onRetry}>
                <RefreshCw data-icon="inline-start" />
                {t("state.retry")}
              </Button>
            ) : null}
          </CardContent>
        ) : null}
      </Card>
    </main>
  )
}
