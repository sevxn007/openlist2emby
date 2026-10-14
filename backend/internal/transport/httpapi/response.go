package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, data any) {
	writeData(w, http.StatusOK, data)
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, envelope{
		Code:    status,
		Message: "success",
		Data:    data,
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, envelope{
		Code:    status,
		Message: message,
		Data:    nil,
	})
}

func writeServiceError(w http.ResponseWriter, err error, fallback int) {
	writeError(w, app.StatusCode(err, fallback), app.PublicMessage(err))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, session domain.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	if r == nil {
		return false
	}
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}
