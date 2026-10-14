import React from "react"
import ReactDOM from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import "@fontsource-variable/geist"

import { App } from "./App"
import { Toaster } from "./components/ui/sonner"
import { TooltipProvider } from "./components/ui/tooltip"
import { I18nProvider } from "./shared/i18n/I18nProvider"
import { ThemeProvider } from "./shared/theme/ThemeProvider"
import "./styles.css"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 15_000,
    },
  },
})

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        <I18nProvider>
          <QueryClientProvider client={queryClient}>
            <App />
            <Toaster />
          </QueryClientProvider>
        </I18nProvider>
      </TooltipProvider>
    </ThemeProvider>
  </React.StrictMode>,
)
