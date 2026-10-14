package app

import (
	"context"
	"io"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type Store interface {
	IdentityInstance(context.Context, string) (string, error)
	FindIdentity(context.Context, string, int64) (domain.LocalUser, error)
	SaveIdentitySetup(context.Context, string, string, domain.OpenListIdentity) error
	SetSessionType(context.Context, string, bool) error
	SessionType(context.Context, string) (bool, error)
	UpdateIdentityName(context.Context, int64, string) error
	GetSettings(context.Context) (domain.Settings, error)
	UpdateSettings(context.Context, string, string, ...string) error
	SetMediaLibraryConfiguration(context.Context, bool, int) error
	GetLocalUserByID(context.Context, int64) (domain.LocalUser, error)
	ListLocalUsers(context.Context) ([]domain.LocalUser, error)
	CreateUser(context.Context, domain.LocalUser) (domain.LocalUser, error)
	UpdateUser(context.Context, domain.LocalUser) (domain.LocalUser, error)
	RevokeUserSessions(context.Context, int64) error
	CreateSessionForUser(context.Context, int64, time.Duration) (domain.Session, error)
	GetSession(context.Context, string) (domain.Session, error)
	RenewSession(context.Context, string, bool, time.Duration) (domain.Session, error)
	DeleteSession(context.Context, string) error
}

type MediaLibraryRuntime interface {
	Reconfigure(context.Context, bool, int) error
}

type MusicMediaItemResolver interface {
	ResolveMusicMediaPath(context.Context, int64) (string, error)
}

// UserTokenRevoker is implemented by optional external authentication stores
// such as the Emby token repository. It lets account lifecycle changes revoke
// device credentials without coupling app.Service to that package.
type UserTokenRevoker interface {
	RevokeUserTokens(context.Context, int64) error
}

type OpenListClient interface {
	TestConnection(context.Context, string, string) (domain.TestConnectionResult, error)
	ProxyJSON(context.Context, string, string, string, []byte) ([]byte, error)
	ListTasks(context.Context, string, string, string) ([]domain.OpenListTask, error)
	CancelTask(context.Context, string, string, string, string) error
	DownloadFile(context.Context, string, string, string, io.Writer, domain.TransferProgress) (domain.OpenListFile, error)
	UploadFile(context.Context, string, string, string, io.Reader, int64, string, domain.TransferProgress) error
}

type MediaService interface {
	Test(context.Context, string) error
	Search(context.Context, string, string, string) ([]domain.MediaSearchResult, error)
	Detail(context.Context, string, string, int64, *int, string) (domain.MediaDetail, error)
}

type MusicService interface {
	Search(context.Context, string, string, int) ([]domain.MusicTrack, error)
	Detail(context.Context, string, string, string) (domain.MusicDetail, error)
	Resolve(context.Context, string, string, string) (domain.MusicAssets, error)
}

type AudioTagger interface {
	Supports(string) bool
	Embed(string, domain.AudioMetadata) error
}

type IdentityClient interface {
	Login(context.Context, string, string, string, string) (string, domain.OpenListIdentity, error)
	Current(context.Context, string, string) (domain.OpenListIdentity, error)
	Users(context.Context, string, string, int, int) (domain.OpenListIdentityPage, error)
	User(context.Context, string, string, int64) (domain.OpenListIdentity, error)
}
