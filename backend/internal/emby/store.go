package emby

import "context"

type TokenInput struct {
	IdentitySession string
	UserID          int64
	DeviceID        string
	DeviceName      string
	ClientName      string
	ClientVersion   string
}

// TokenIdentity identifies the authorization record that owns an Emby device token.
// It deliberately contains no cached authorization data: callers must load the
// current authorization record for every request so role and enabled-state changes apply
// immediately.
type TokenIdentity struct {
	IdentitySession string
	UserID          int64
}

type TokenStore interface {
	CreateEmbyToken(context.Context, string, TokenInput) error
	GetEmbyTokenIdentity(context.Context, string) (TokenIdentity, bool, error)
	RevokeEmbyToken(context.Context, string) error
	RevokeEmbyTokensForUser(context.Context, int64) error
	GetOrCreateEmbyServerID(context.Context, string) (string, error)
}
