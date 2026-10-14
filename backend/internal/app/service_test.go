package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type settingsFailureStore struct {
	Store
	settingsErr error
}

func (s *settingsFailureStore) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{
		OpenListBaseURL: "http://openlist.local",
		EmbyPort:        domain.DefaultEmbyPort,
	}, nil
}

func (s *settingsFailureStore) UpdateSettings(context.Context, string, string, ...string) error {
	return s.settingsErr
}

func TestUpdateSettingsReturnsPersistenceError(t *testing.T) {
	want := errors.New("synthetic settings persistence failure")
	service := NewService(&settingsFailureStore{settingsErr: want}, nil, nil, nil, nil)

	_, err := service.UpdateSettings(t.Context(), SettingsInput{
		OpenListBaseURL: "http://openlist.local",
	})
	if !errors.Is(err, want) {
		t.Fatalf("UpdateSettings() error = %v, want %v", err, want)
	}
}
