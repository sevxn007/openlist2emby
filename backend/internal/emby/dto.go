package emby

type publicSystemInfo struct {
	LocalAddress           string `json:"LocalAddress"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	OperatingSystem        string `json:"OperatingSystem"`
	Id                     string `json:"Id"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

type systemInfo struct {
	publicSystemInfo
	WanAddress             string   `json:"WanAddress,omitempty"`
	WebSocketPortNumber    int      `json:"WebSocketPortNumber"`
	CompletedInstallations []string `json:"CompletedInstallations"`
	HasUpdateAvailable     bool     `json:"HasUpdateAvailable"`
	CanSelfRestart         bool     `json:"CanSelfRestart"`
	CanLaunchWebBrowser    bool     `json:"CanLaunchWebBrowser"`
}

type userDTO struct {
	Name                  string            `json:"Name"`
	ServerId              string            `json:"ServerId"`
	Id                    string            `json:"Id"`
	HasPassword           bool              `json:"HasPassword"`
	HasConfiguredPassword bool              `json:"HasConfiguredPassword"`
	Configuration         userConfiguration `json:"Configuration"`
	Policy                userPolicy        `json:"Policy"`
}

type userConfiguration struct {
	AudioLanguagePreference    string   `json:"AudioLanguagePreference"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	GroupedFolders             []string `json:"GroupedFolders"`
	SubtitleMode               string   `json:"SubtitleMode"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	OrderedViews               []string `json:"OrderedViews"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
}

type userPolicy struct {
	IsAdministrator                 bool     `json:"IsAdministrator"`
	IsHidden                        bool     `json:"IsHidden"`
	IsDisabled                      bool     `json:"IsDisabled"`
	EnableUserPreferenceAccess      bool     `json:"EnableUserPreferenceAccess"`
	EnableRemoteControlOfOtherUsers bool     `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess              bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement          bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess              bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback             bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion           bool     `json:"EnableContentDeletion"`
	EnableContentDownloading        bool     `json:"EnableContentDownloading"`
	EnableSyncTranscoding           bool     `json:"EnableSyncTranscoding"`
	EnableAllDevices                bool     `json:"EnableAllDevices"`
	EnableAllChannels               bool     `json:"EnableAllChannels"`
	EnableAllFolders                bool     `json:"EnableAllFolders"`
	BlockedTags                     []string `json:"BlockedTags"`
	AllowedTags                     []string `json:"AllowedTags"`
	EnabledDevices                  []string `json:"EnabledDevices"`
	EnabledChannels                 []string `json:"EnabledChannels"`
	EnabledFolders                  []string `json:"EnabledFolders"`
	InvalidLoginAttemptCount        int      `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout      int      `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions               int      `json:"MaxActiveSessions"`
	RemoteClientBitrateLimit        int      `json:"RemoteClientBitrateLimit"`
	AuthenticationProviderId        string   `json:"AuthenticationProviderId"`
	PasswordResetProviderId         string   `json:"PasswordResetProviderId"`
}

type authenticationResult struct {
	User        userDTO     `json:"User"`
	SessionInfo sessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerId    string      `json:"ServerId"`
}

type sessionInfo struct {
	PlayState             sessionPlayState    `json:"PlayState"`
	AdditionalUsers       []string            `json:"AdditionalUsers"`
	Capabilities          sessionCapabilities `json:"Capabilities"`
	RemoteEndPoint        string              `json:"RemoteEndPoint"`
	PlayableMediaTypes    []string            `json:"PlayableMediaTypes"`
	Id                    string              `json:"Id"`
	UserId                string              `json:"UserId"`
	UserName              string              `json:"UserName"`
	Client                string              `json:"Client"`
	LastActivityDate      string              `json:"LastActivityDate"`
	LastPlaybackCheckIn   string              `json:"LastPlaybackCheckIn"`
	DeviceName            string              `json:"DeviceName"`
	DeviceId              string              `json:"DeviceId"`
	ApplicationVersion    string              `json:"ApplicationVersion"`
	IsActive              bool                `json:"IsActive"`
	SupportsMediaControl  bool                `json:"SupportsMediaControl"`
	SupportsRemoteControl bool                `json:"SupportsRemoteControl"`
}

type sessionPlayState struct {
	CanSeek    bool   `json:"CanSeek"`
	IsPaused   bool   `json:"IsPaused"`
	IsMuted    bool   `json:"IsMuted"`
	RepeatMode string `json:"RepeatMode"`
}

type sessionCapabilities struct {
	PlayableMediaTypes           []string `json:"PlayableMediaTypes"`
	SupportedCommands            []string `json:"SupportedCommands"`
	SupportsMediaControl         bool     `json:"SupportsMediaControl"`
	SupportsContentUploading     bool     `json:"SupportsContentUploading"`
	SupportsPersistentIdentifier bool     `json:"SupportsPersistentIdentifier"`
	SupportsSync                 bool     `json:"SupportsSync"`
}

type queryResult struct {
	Items            []baseItemDTO `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
	StartIndex       int           `json:"StartIndex"`
}

// baseItemPerson follows Emby's People endpoint shape. PrimaryImageTag is a
// stable local tag that lets clients request the TMDB profile through the
// standard /Persons/{id}/Images/Primary endpoint.
type baseItemPerson struct {
	Name                    string  `json:"Name"`
	Id                      string  `json:"Id"`
	Role                    string  `json:"Role,omitempty"`
	Type                    string  `json:"Type"`
	PrimaryImageTag         string  `json:"PrimaryImageTag,omitempty"`
	PrimaryImageAspectRatio float64 `json:"PrimaryImageAspectRatio,omitempty"`
}

type nameIDPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type imageInfoDTO struct {
	ImageType  string `json:"ImageType"`
	ImageIndex int    `json:"ImageIndex"`
	Path       string `json:"Path"`
	Filename   string `json:"Filename"`
	Height     int    `json:"Height"`
	Width      int    `json:"Width"`
	Size       int64  `json:"Size"`
}

type baseItemDTO struct {
	Name                     string            `json:"Name"`
	ServerId                 string            `json:"ServerId"`
	Id                       string            `json:"Id"`
	ParentId                 string            `json:"ParentId,omitempty"`
	Etag                     string            `json:"Etag"`
	DateCreated              string            `json:"DateCreated"`
	SortName                 string            `json:"SortName"`
	Overview                 string            `json:"Overview,omitempty"`
	Genres                   []string          `json:"Genres"`
	PremiereDate             string            `json:"PremiereDate,omitempty"`
	CommunityRating          float64           `json:"CommunityRating,omitempty"`
	RunTimeTicks             int64             `json:"RunTimeTicks,omitempty"`
	ProductionYear           *int              `json:"ProductionYear,omitempty"`
	ProviderIds              map[string]string `json:"ProviderIds"`
	IsFolder                 bool              `json:"IsFolder"`
	Type                     string            `json:"Type"`
	MediaType                string            `json:"MediaType,omitempty"`
	Container                string            `json:"Container,omitempty"`
	Path                     string            `json:"Path,omitempty"`
	MediaSourceCount         int               `json:"MediaSourceCount,omitempty"`
	LocationType             string            `json:"LocationType"`
	CanDelete                bool              `json:"CanDelete"`
	CanDownload              bool              `json:"CanDownload"`
	SupportsSync             bool              `json:"SupportsSync"`
	ImageTags                map[string]string `json:"ImageTags"`
	BackdropImageTags        []string          `json:"BackdropImageTags"`
	UserData                 userItemDataDTO   `json:"UserData"`
	IndexNumber              *int              `json:"IndexNumber,omitempty"`
	ParentIndexNumber        *int              `json:"ParentIndexNumber,omitempty"`
	SeriesId                 string            `json:"SeriesId,omitempty"`
	SeriesName               string            `json:"SeriesName,omitempty"`
	SeriesPrimaryImageTag    string            `json:"SeriesPrimaryImageTag,omitempty"`
	SeasonId                 string            `json:"SeasonId,omitempty"`
	SeasonName               string            `json:"SeasonName,omitempty"`
	Album                    string            `json:"Album,omitempty"`
	AlbumId                  string            `json:"AlbumId,omitempty"`
	AlbumPrimaryImageTag     string            `json:"AlbumPrimaryImageTag,omitempty"`
	AlbumArtist              string            `json:"AlbumArtist,omitempty"`
	Artists                  []string          `json:"Artists,omitempty"`
	ArtistItems              []nameIDPair      `json:"ArtistItems,omitempty"`
	AlbumArtists             []nameIDPair      `json:"AlbumArtists,omitempty"`
	PrimaryImageItemId       string            `json:"PrimaryImageItemId,omitempty"`
	PrimaryImageTag          string            `json:"PrimaryImageTag,omitempty"`
	ParentPrimaryImageItemId string            `json:"ParentPrimaryImageItemId,omitempty"`
	ParentPrimaryImageTag    string            `json:"ParentPrimaryImageTag,omitempty"`
	HasLyrics                bool              `json:"HasLyrics,omitempty"`
	AlbumCount               int               `json:"AlbumCount,omitempty"`
	SongCount                int               `json:"SongCount,omitempty"`
	ChildCount               int               `json:"ChildCount,omitempty"`
	MediaSources             []mediaSourceInfo `json:"MediaSources,omitempty"`
	People                   []baseItemPerson  `json:"People,omitempty"`
	CollectionType           string            `json:"CollectionType,omitempty"`
}

type userItemDataDTO struct {
	ItemId                string  `json:"ItemId,omitempty"`
	PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
	PlayCount             int     `json:"PlayCount"`
	IsFavorite            bool    `json:"IsFavorite"`
	Played                bool    `json:"Played"`
	PlayedPercentage      float64 `json:"PlayedPercentage,omitempty"`
	LastPlayedDate        string  `json:"LastPlayedDate,omitempty"`
	UnplayedItemCount     *int    `json:"UnplayedItemCount,omitempty"`
}

type playbackInfoResponse struct {
	MediaSources  []mediaSourceInfo `json:"MediaSources"`
	PlaySessionId string            `json:"PlaySessionId"`
	ErrorCode     string            `json:"ErrorCode,omitempty"`
}

type mediaSourceInfo struct {
	Protocol              string            `json:"Protocol"`
	Id                    string            `json:"Id"`
	Path                  string            `json:"Path"`
	Type                  string            `json:"Type"`
	Container             string            `json:"Container"`
	Size                  int64             `json:"Size"`
	Name                  string            `json:"Name"`
	IsRemote              bool              `json:"IsRemote"`
	RunTimeTicks          int64             `json:"RunTimeTicks,omitempty"`
	SupportsDirectPlay    bool              `json:"SupportsDirectPlay"`
	SupportsDirectStream  bool              `json:"SupportsDirectStream"`
	SupportsTranscoding   bool              `json:"SupportsTranscoding"`
	DirectStreamUrl       string            `json:"DirectStreamUrl"`
	MediaStreams          []mediaStream     `json:"MediaStreams"`
	RequiredHttpHeaders   map[string]string `json:"RequiredHttpHeaders"`
	ReadAtNativeFramerate bool              `json:"ReadAtNativeFramerate"`
	IsInfiniteStream      bool              `json:"IsInfiniteStream"`
	RequiresOpening       bool              `json:"RequiresOpening"`
	RequiresClosing       bool              `json:"RequiresClosing"`
	RequiresLooping       bool              `json:"RequiresLooping"`
	SupportsProbing       bool              `json:"SupportsProbing"`
}

type mediaStream struct {
	Codec      string `json:"Codec,omitempty"`
	Type       string `json:"Type"`
	Index      int    `json:"Index"`
	IsDefault  bool   `json:"IsDefault"`
	IsExternal bool   `json:"IsExternal"`
}

type playbackProgressRequest struct {
	ItemId        string `json:"ItemId"`
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	PlaySessionId string `json:"PlaySessionId"`
}

type errorResponse struct {
	ResponseStatus responseStatus `json:"ResponseStatus"`
}

type responseStatus struct {
	ErrorCode  string   `json:"ErrorCode"`
	Message    string   `json:"Message"`
	StackTrace string   `json:"StackTrace"`
	Errors     []string `json:"Errors"`
}
