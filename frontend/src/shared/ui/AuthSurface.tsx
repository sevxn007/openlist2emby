import { ReactNode } from "react"
import { ServerCog } from "lucide-react"

import {
  Card,
  CardContent,
  CardHeader,
} from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { useI18n } from "@/shared/i18n/useI18n"
import { ThemeMenu } from "@/shared/ui/ThemeMenu"

interface AuthSurfaceProps {
  title: string
  children: ReactNode
  meta?: ReactNode
  className?: string
}

export function AuthSurface({ title, children, meta, className }: AuthSurfaceProps) {
  const { t } = useI18n()

  return (
    <main className="relative grid min-h-svh place-items-center bg-background px-4 py-10">
      <div className="absolute top-4 right-4">
        <ThemeMenu />
      </div>
      <section className={cn("flex w-full max-w-sm flex-col gap-6", className)}>
        <div className="flex items-center gap-3" aria-label={t("app.brand")}>
          <div className="flex size-10 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
            <ServerCog className="size-5" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-base font-semibold">{t("app.brand")}</div>
            {meta ? (
              <div className="truncate text-xs text-muted-foreground">{meta}</div>
            ) : null}
          </div>
        </div>

        <Card className="shadow-sm">
          <CardHeader>
            <h1 className="font-heading text-lg leading-snug font-medium">{title}</h1>
          </CardHeader>
          <CardContent>{children}</CardContent>
        </Card>
      </section>
    </main>
  )
}
