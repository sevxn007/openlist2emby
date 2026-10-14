import { Monitor, Moon, Sun } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useI18n } from "@/shared/i18n/useI18n"
import { ThemePreference } from "@/shared/theme/context"
import { useTheme } from "@/shared/theme/useTheme"

const themeOptions = [
  { value: "system" as const, labelKey: "app.theme.system" as const, icon: Monitor },
  { value: "light" as const, labelKey: "app.theme.light" as const, icon: Sun },
  { value: "dark" as const, labelKey: "app.theme.dark" as const, icon: Moon },
]

export function ThemeMenu() {
  const { t } = useI18n()
  const { theme, setTheme } = useTheme()
  const CurrentIcon = themeOptions.find((option) => option.value === theme)?.icon ?? Monitor

  return (
    <Tooltip>
      <DropdownMenu>
        <TooltipTrigger asChild>
          <span className="inline-flex">
            <DropdownMenuTrigger asChild>
              <Button aria-label={t("app.theme")} variant="outline" size="icon-lg">
                <CurrentIcon />
              </Button>
            </DropdownMenuTrigger>
          </span>
        </TooltipTrigger>
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuRadioGroup
            value={theme}
            onValueChange={(value) => setTheme(value as ThemePreference)}
          >
            {themeOptions.map((option) => {
              const Icon = option.icon
              return (
                <DropdownMenuRadioItem
                  className="min-h-11 gap-2 px-2.5"
                  key={option.value}
                  value={option.value}
                >
                  <Icon />
                  {t(option.labelKey)}
                </DropdownMenuRadioItem>
              )
            })}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <TooltipContent>{t("app.theme")}</TooltipContent>
    </Tooltip>
  )
}
