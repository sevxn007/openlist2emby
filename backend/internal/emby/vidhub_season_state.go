package emby

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const vidHubSeasonSelectionTTL = 10 * time.Minute

type vidHubSeasonKey struct {
	userID   int64
	token    string
	seriesID int64
}

type vidHubSeasonSelection struct {
	seasonID int64
	expires  time.Time
}

func (s *Server) rememberVidHubSeason(r *http.Request, viewer viewer, token string, seriesID, seasonID int64) {
	if s == nil || !isVidHubRequest(r) || token == "" || seriesID <= 0 || seasonID <= 0 {
		return
	}
	now := time.Now()
	key := vidHubSeasonKey{userID: viewer.id(), token: tokenHash(token), seriesID: seriesID}

	s.vidHubMu.Lock()
	defer s.vidHubMu.Unlock()
	if s.vidHubSeasons == nil {
		s.vidHubSeasons = make(map[vidHubSeasonKey]vidHubSeasonSelection)
	}
	for existingKey, selection := range s.vidHubSeasons {
		if !selection.expires.After(now) {
			delete(s.vidHubSeasons, existingKey)
		}
	}
	s.vidHubSeasons[key] = vidHubSeasonSelection{seasonID: seasonID, expires: now.Add(vidHubSeasonSelectionTTL)}
}

func (s *Server) takeVidHubSeason(r *http.Request, viewer viewer, token string, seriesID int64) string {
	if s == nil || !isVidHubRequest(r) || token == "" || seriesID <= 0 {
		return ""
	}
	key := vidHubSeasonKey{userID: viewer.id(), token: tokenHash(token), seriesID: seriesID}

	s.vidHubMu.Lock()
	selection, ok := s.vidHubSeasons[key]
	if ok {
		delete(s.vidHubSeasons, key)
	}
	s.vidHubMu.Unlock()
	if !ok || !selection.expires.After(time.Now()) {
		return ""
	}
	return strconv.FormatInt(selection.seasonID, 10)
}

func isVidHubRequest(r *http.Request) bool {
	return r != nil && strings.Contains(strings.ToLower(r.UserAgent()), "vidhub")
}
