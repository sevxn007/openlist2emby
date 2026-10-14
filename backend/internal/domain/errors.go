package domain

import "errors"

var (
	ErrAlreadySetup         = errors.New("openlist2emby is already set up")
	ErrNotSetup             = errors.New("openlist2emby is not set up")
	ErrNotFound             = errors.New("record not found")
	ErrTMDBKeyNotConfigured = errors.New("tmdb api key is not configured")
	ErrUnsupportedAudio     = errors.New("audio format is not supported")
	ErrFragmentedAudio      = errors.New("fragmented M4A files cannot be updated")
	ErrForbidden            = errors.New("permission denied")
	ErrLastAdministrator    = errors.New("at least one enabled administrator is required")
)
