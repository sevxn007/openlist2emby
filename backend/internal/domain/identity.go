package domain

// OpenListIdentity is a whitelist, not a mirror of the upstream user model.
type OpenListIdentity struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     int    `json:"role"`
	Disabled bool   `json:"disabled"`
	OTP      bool   `json:"otp"`
}
type OpenListIdentityPage struct {
	Content []OpenListIdentity `json:"content"`
	Total   int                `json:"total"`
}
