import { cn } from "@/lib/utils"
import { Loader2Icon } from "lucide-react"
import { useI18n } from "@/shared/i18n/useI18n"

function Spinner({
  className,
  "aria-label": ariaLabel,
  ...props
}: React.ComponentProps<"svg">) {
  const { t } = useI18n()

  return (
    <Loader2Icon
      data-slot="spinner"
      role="status"
      aria-label={ariaLabel ?? t("common.loading")}
      className={cn("size-4 animate-spin", className)}
      {...props}
    />
  )
}

export { Spinner }
