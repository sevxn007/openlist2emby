import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render } from "@testing-library/react"
import { ReactNode } from "react"

import { TooltipProvider } from "@/components/ui/tooltip"
import { I18nProvider } from "@/shared/i18n/I18nProvider"
import { ThemeProvider } from "@/shared/theme/ThemeProvider"

export function renderWithProviders(children: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: {
        retry: false,
      },
      queries: {
        retry: false,
      },
    },
  })

  return render(
    <ThemeProvider>
      <TooltipProvider delayDuration={0}>
        <I18nProvider>
          <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
        </I18nProvider>
      </TooltipProvider>
    </ThemeProvider>,
  )
}
