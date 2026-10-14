import { FormEvent, useEffect, useRef, useState } from "react"
import { ArrowLeft, ArrowRight, Check, Server, UserRound } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Carousel,
  type CarouselApi,
  CarouselContent,
  CarouselItem,
} from "@/components/ui/carousel"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"
import { ManagerApiError, setupManager } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { AuthSurface } from "@/shared/ui/AuthSurface"

interface SetupPageProps {
  onSetupComplete: () => void
}

export function SetupPage({ onSetupComplete }: SetupPageProps) {
  const { t } = useI18n()
  const [activeStep, setActiveStep] = useState(0)
  const [carouselApi, setCarouselApi] = useState<CarouselApi>()
  const [openListBaseURL, setOpenListBaseURL] = useState("http://localhost:5244")
  const [openListAdminKey, setOpenListAdminKey] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [otpCode, setOTPCode] = useState("")
  const [error, setError] = useState("")
  const [isSaving, setIsSaving] = useState(false)
  const openListBaseURLRef = useRef<HTMLInputElement>(null)
  const usernameRef = useRef<HTMLInputElement>(null)

  const steps = [
    {
      label: t("auth.setup.step.openList"),
      icon: Server,
    },
    {
      label: t("auth.setup.step.account"),
      icon: UserRound,
    },
  ]

  useEffect(() => {
    carouselApi?.scrollTo(activeStep)
    if (activeStep === 0) {
      openListBaseURLRef.current?.focus()
    } else {
      usernameRef.current?.focus()
    }
  }, [activeStep, carouselApi])

  function showStep(step: number) {
    setError("")
    setActiveStep(step)
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (activeStep === 0) {
      showStep(1)
      return
    }

    setError("")
    setIsSaving(true)
    try {
      await setupManager({
        openlist_base_url: openListBaseURL,
        openlist_admin_key: openListAdminKey,
        username,
        password,
        ...(otpCode ? { otp_code: otpCode } : {}),
      })
      onSetupComplete()
    } catch (err) {
      setError(err instanceof ManagerApiError ? err.message : t("auth.setup.failed"))
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <AuthSurface title={t("auth.setup.title")} className="max-w-md">
      <form onSubmit={handleSubmit}>
        <div
          role="progressbar"
          aria-label={t("auth.setup.progress")}
          aria-valuemin={1}
          aria-valuemax={steps.length}
          aria-valuenow={activeStep + 1}
          aria-valuetext={t("auth.setup.stepStatus", {
            current: activeStep + 1,
            total: steps.length,
            step: steps[activeStep].label,
          })}
          className="mb-6"
        >
          <div className="relative grid grid-cols-2">
            <div
              aria-hidden="true"
              className="absolute top-[18px] right-1/4 left-1/4 h-px bg-border"
            >
              <span
                className={cn(
                  "block h-full origin-left bg-primary transition-transform duration-300",
                  activeStep === 0 ? "scale-x-0" : "scale-x-100",
                )}
              />
            </div>
            {steps.map((step, index) => {
              const StepIcon = step.icon
              const isComplete = index < activeStep
              const isCurrent = index === activeStep

              return (
                <div
                  key={step.label}
                  className="relative z-10 flex min-w-0 flex-col items-center gap-2"
                >
                  <span
                    className={cn(
                      "grid size-9 place-items-center rounded-full border bg-card transition-colors duration-200",
                      (isComplete || isCurrent) &&
                        "border-primary bg-primary text-primary-foreground",
                      !isComplete && !isCurrent && "text-muted-foreground",
                    )}
                  >
                    {isComplete ? (
                      <Check className="size-4" aria-hidden="true" />
                    ) : (
                      <StepIcon className="size-4" aria-hidden="true" />
                    )}
                  </span>
                  <span
                    className={cn(
                      "text-center text-xs font-medium",
                      !isComplete && !isCurrent && "text-muted-foreground",
                    )}
                  >
                    {step.label}
                  </span>
                </div>
              )
            })}
          </div>
        </div>

        <Carousel
          aria-label={t("auth.setup.title")}
          opts={{ align: "start", watchDrag: false }}
          setApi={setCarouselApi}
        >
          <CarouselContent>
            <CarouselItem
              aria-hidden={activeStep !== 0}
              className={cn(
                "transition-opacity duration-200",
                activeStep !== 0 && "invisible opacity-0",
              )}
            >
              <div className="flex min-h-[21rem] flex-col">
                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="setup-openlist-url">
                      {t("auth.setup.openListAddress")}
                    </FieldLabel>
                    <Input
                      ref={openListBaseURLRef}
                      id="setup-openlist-url"
                      value={openListBaseURL}
                      onChange={(event) => setOpenListBaseURL(event.target.value)}
                      placeholder="http://localhost:5244"
                      type="url"
                      disabled={activeStep !== 0}
                      required
                    />
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="setup-admin-key">
                      {t("auth.setup.adminKey")}
                    </FieldLabel>
                    <Input
                      id="setup-admin-key"
                      value={openListAdminKey}
                      onChange={(event) => setOpenListAdminKey(event.target.value)}
                      placeholder={t("auth.setup.adminKeyPlaceholder")}
                      type="password"
                      autoComplete="off"
                      spellCheck={false}
                      disabled={activeStep !== 0}
                      required
                    />
                  </Field>
                </FieldGroup>

                <div className="mt-auto flex justify-end pt-5">
                  <Button type="submit" size="lg">
                    {t("auth.setup.next")}
                    <ArrowRight data-icon="inline-end" aria-hidden="true" />
                  </Button>
                </div>
              </div>
            </CarouselItem>

            <CarouselItem
              aria-hidden={activeStep !== 1}
              className={cn(
                "transition-opacity duration-200",
                activeStep !== 1 && "invisible opacity-0",
              )}
            >
              <div className="flex min-h-[21rem] flex-col">
                <FieldGroup>
                  <Field data-invalid={Boolean(error)}>
                    <FieldLabel htmlFor="setup-username">
                      {t("auth.login.username")}
                    </FieldLabel>
                    <Input
                      ref={usernameRef}
                      id="setup-username"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      autoComplete="username"
                      aria-invalid={Boolean(error)}
                      disabled={activeStep !== 1}
                      required
                    />
                  </Field>

                  <Field data-invalid={Boolean(error)}>
                    <FieldLabel htmlFor="setup-password">
                      {t("auth.login.password")}
                    </FieldLabel>
                    <Input
                      id="setup-password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      autoComplete="current-password"
                      type="password"
                      aria-invalid={Boolean(error)}
                      disabled={activeStep !== 1}
                      required
                    />
                  </Field>

                  <Field data-invalid={Boolean(error)}>
                    <FieldLabel htmlFor="setup-otp">
                      {t("auth.otp")}
                    </FieldLabel>
                    <Input
                      id="setup-otp"
                      value={otpCode}
                      onChange={(event) => {
                        setOTPCode(event.target.value)
                        setError("")
                      }}
                      autoComplete="one-time-code"
                      inputMode="numeric"
                      maxLength={6}
                      aria-invalid={Boolean(error)}
                      disabled={activeStep !== 1}
                    />
                    {error ? <FieldError>{error}</FieldError> : null}
                  </Field>
                </FieldGroup>

                <div className="mt-auto grid grid-cols-2 gap-3 pt-5">
                  <Button
                    type="button"
                    variant="outline"
                    size="lg"
                    onClick={() => showStep(0)}
                    disabled={isSaving}
                  >
                    <ArrowLeft data-icon="inline-start" aria-hidden="true" />
                    {t("auth.setup.back")}
                  </Button>
                  <Button type="submit" size="lg" disabled={isSaving}>
                    {isSaving ? <Spinner data-icon="inline-start" /> : null}
                    {t("auth.setup.submit")}
                  </Button>
                </div>
              </div>
            </CarouselItem>
          </CarouselContent>
        </Carousel>
      </form>
    </AuthSurface>
  )
}
