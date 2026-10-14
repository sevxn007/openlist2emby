package sqlite

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
)

func TestUsersPersistRolesGrantsAndDisabledState(t *testing.T) {
	store, _ := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "users.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}

	mediaUser, err := store.CreateUser(t.Context(), domain.LocalUser{OpenListUserID: 101, InstanceID: "test-instance",
		Username: "viewer", Role: domain.LocalUserRoleMedia, Enabled: false,
		LibraryGrants: []domain.LibraryGrant{{LibraryID: library.ID, Access: domain.LibraryAccessEdit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mediaUser.Enabled || mediaUser.Role != domain.LocalUserRoleMedia || len(mediaUser.LibraryGrants) != 1 || mediaUser.LibraryGrants[0] != (domain.LibraryGrant{LibraryID: library.ID, Access: domain.LibraryAccessEdit}) {
		t.Fatalf("created media user = %#v", mediaUser)
	}
	if _, err := store.CreateSessionForUser(t.Context(), mediaUser.ID, time.Hour); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("disabled user session error = %v, want ErrNotFound", err)
	}
	if granted, err := store.UserCanAccessLibrary(t.Context(), mediaUser.ID, library.ID); err != nil || !granted {
		t.Fatalf("grant = (%t, %v), want (true, nil)", granted, err)
	}
	if granted, err := store.UserCanEditLibrary(t.Context(), mediaUser.ID, library.ID); err != nil || !granted {
		t.Fatalf("edit grant = (%t, %v), want (true, nil)", granted, err)
	}

	mediaUser.Enabled = true
	mediaUser, err = store.UpdateUser(t.Context(), mediaUser)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSessionForUser(t.Context(), mediaUser.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mediaUser.Enabled = false
	if _, err := store.UpdateUser(t.Context(), mediaUser); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(t.Context(), session.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("disabled user retained session: %v", err)
	}
}

func TestAdministratorLibraryGrantsControlVisibilityWithoutChangingAccess(t *testing.T) {
	store, _ := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "administrator-grants.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIdentitySetup(t.Context(), "http://openlist.local", "admin-key", domain.OpenListIdentity{ID: 1, Username: "admin", Role: 2}); err != nil {
		t.Fatal(err)
	}
	movies, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if admin.LibraryGrantsConfigured || len(admin.LibraryGrants) != 1 || admin.LibraryGrants[0] != (domain.LibraryGrant{LibraryID: movies.ID, Access: domain.LibraryAccessView}) {
		t.Fatalf("default administrator grants = %#v", admin)
	}

	admin.LibraryGrantsConfigured = true
	admin.LibraryGrants = []domain.LibraryGrant{}
	admin, err = store.UpdateUser(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if !admin.LibraryGrantsConfigured || len(admin.LibraryGrants) != 0 {
		t.Fatalf("explicit empty administrator grants = %#v", admin)
	}

	music, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Music", ContentType: medialibrary.ContentMusic, RootPath: "/Music",
	})
	if err != nil {
		t.Fatal(err)
	}
	admin, err = store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LibraryGrants) != 1 || admin.LibraryGrants[0] != (domain.LibraryGrant{LibraryID: music.ID, Access: domain.LibraryAccessView}) {
		t.Fatalf("administrator grants after creating library = %#v", admin.LibraryGrants)
	}

	admin.LibraryGrants = []domain.LibraryGrant{{LibraryID: movies.ID, Access: domain.LibraryAccessEdit}}
	admin, err = store.UpdateUser(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LibraryGrants) != 1 || admin.LibraryGrants[0].Access != domain.LibraryAccessView {
		t.Fatalf("normalized administrator grants = %#v", admin.LibraryGrants)
	}
	if err := store.DeleteLibrary(t.Context(), movies.ID); err != nil {
		t.Fatal(err)
	}
	admin, err = store.GetLocalUserByID(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.LibraryGrants) != 0 {
		t.Fatalf("deleted library retained administrator grants = %#v", admin.LibraryGrants)
	}
}

func TestMediaUserDataIsScopedByUser(t *testing.T) {
	store, _ := openUninitializedTestStore(t, filepath.Join(t.TempDir(), "media-user-data.db"))
	if err := store.InitializeSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(t.Context(), medialibrary.LibraryInput{
		Name: "Movies", ContentType: medialibrary.ContentMovies, RootPath: "/Movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginLibraryScan(t.Context(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.UpsertMediaItem(t.Context(), medialibrary.ItemInput{
		LibraryID: library.ID, Kind: medialibrary.KindMovie, Path: "/Movies/Example.mp4", MediaPath: "/Movies/Example.mp4",
		Name: "Example", SortName: "Example", Container: "mp4", Genres: []string{}, Artists: []string{}, ScanGeneration: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 1, item.ID, medialibrary.UserData{PlaybackPositionTicks: 100, Favorite: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMediaUserDataForUser(t.Context(), 2, item.ID, medialibrary.UserData{PlaybackPositionTicks: 200, Played: true}); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetMediaItemForUser(t.Context(), 1, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.GetMediaItemForUser(t.Context(), 2, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.UserData.PlaybackPositionTicks != 100 || !first.UserData.Favorite || first.UserData.Played {
		t.Fatalf("first user data = %#v", first.UserData)
	}
	if second.UserData.PlaybackPositionTicks != 200 || second.UserData.Favorite || !second.UserData.Played {
		t.Fatalf("second user data = %#v", second.UserData)
	}
}
