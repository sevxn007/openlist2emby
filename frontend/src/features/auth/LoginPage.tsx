import { FormEvent, useState } from "react"

import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { login, ManagerApiError } from "@/shared/api/manager"
import { useI18n } from "@/shared/i18n/useI18n"
import { AuthSurface } from "@/shared/ui/AuthSurface"

interface LoginPageProps {
  openListBaseURL?: string
  onLoginComplete: () => void
}

export function LoginPage({ openListBaseURL, onLoginComplete }: LoginPageProps) {
  const { t } = useI18n()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [otpCode, setOTPCode] = useState("")
  const [error, setError] = useState("")
  const [isLoggingIn, setIsLoggingIn] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    setIsLoggingIn(true)
    try {
      await login({ username, password, ...(otpCode ? { otp_code: otpCode } : {}) })
      onLoginComplete()
    } catch (err) {
      setError(err instanceof ManagerApiError ? err.message : t("auth.login.failed"))
    } finally {
      setIsLoggingIn(false)
    }
  }

  return (
    <AuthSurface title={t("auth.login.title")} meta={openListBaseURL}>
      <form onSubmit={handleSubmit}>
        <FieldGroup>
          <Field data-invalid={Boolean(error)}>
            <FieldLabel htmlFor="login-username">
              {t("auth.login.username")}
            </FieldLabel>
            <Input
              id="login-username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="username"
              aria-invalid={Boolean(error)}
              required
            />
          </Field>

          <Field data-invalid={Boolean(error)}>
            <FieldLabel htmlFor="login-password">
              {t("auth.login.password")}
            </FieldLabel>
            <Input
              id="login-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              type="password"
              aria-invalid={Boolean(error)}
              required
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="login-otp">{t("auth.otp")}</FieldLabel>
            <Input
              id="login-otp"
              value={otpCode}
              onChange={(event) => setOTPCode(event.target.value)}
              autoComplete="one-time-code"
              inputMode="numeric"
              maxLength={6}
            />
          </Field>
          {error ? <FieldError>{error}</FieldError> : null}

          <Button type="submit" size="lg" disabled={isLoggingIn}>
            {isLoggingIn ? <Spinner data-icon="inline-start" /> : null}
            {t("auth.login.submit")}
          </Button>
        </FieldGroup>
      </form>
    </AuthSurface>
  )
}
