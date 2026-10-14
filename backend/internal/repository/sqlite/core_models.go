package sqlite

type settingsModel struct {
	TVDBAPIKey           string          `gorm:"column:tvdb_api_key;not null;default:''"`
	TVDBPIN              string          `gorm:"column:tvdb_pin;not null;default:''"`
	MetadataPriorityJSON string          `gorm:"not null;default:''"`
	ID                   int64           `gorm:"primaryKey;autoIncrement:false;check:check_settings_singleton,id = 1"`
	OpenListBaseURL      string          `gorm:"column:openlist_base_url;not null"`
	OpenListAdminKey     string          `gorm:"column:openlist_admin_key;not null"`
	TMDBAPIKey           string          `gorm:"column:tmdb_api_key;not null;default:''"`
	MediaLibraryEnabled  bool            `gorm:"not null;default:false"`
	EmbyPort             int             `gorm:"not null;default:8096"`
	MediaMetadataPath    string          `gorm:"not null;default:''"`
	CreatedAt            sqliteTimestamp `gorm:"not null"`
	UpdatedAt            sqliteTimestamp `gorm:"not null"`
}

func (settingsModel) TableName() string { return "settings" }

type localUserModel struct {
	ID                      int64           `gorm:"primaryKey;autoIncrement"`
	Username                string          `gorm:"not null"`
	OpenListUserID          int64           `gorm:"not null;check:check_identity_user_id,open_list_user_id > 0;uniqueIndex:idx_identity_binding"`
	InstanceID              string          `gorm:"not null;uniqueIndex:idx_identity_binding"`
	Role                    string          `gorm:"not null;default:'media';check:check_user_authorizations_role,role IN ('admin','media')"`
	Enabled                 bool            `gorm:"not null;index:idx_user_authorizations_enabled"`
	LibraryGrantsConfigured bool            `gorm:"not null;default:false"`
	CreatedAt               sqliteTimestamp `gorm:"not null"`
	UpdatedAt               sqliteTimestamp `gorm:"not null"`
}

func (localUserModel) TableName() string { return "user_authorizations" }

type sessionModel struct {
	Token         string          `gorm:"primaryKey"`
	UpstreamToken string          `gorm:"not null;default:''"`
	Emby          bool            `gorm:"not null;default:false"`
	UserID        int64           `gorm:"not null;index:idx_sessions_user"`
	ExpiresAt     sqliteTimestamp `gorm:"not null"`
	CreatedAt     sqliteTimestamp `gorm:"not null"`
	UpdatedAt     sqliteTimestamp `gorm:"not null"`
}

func (sessionModel) TableName() string { return "sessions" }

type userLibraryGrantModel struct {
	UserID      int64           `gorm:"primaryKey;autoIncrement:false;index:idx_user_library_grants_user"`
	LibraryID   int64           `gorm:"primaryKey;autoIncrement:false;index:idx_user_library_grants_library"`
	AccessLevel string          `gorm:"column:access_level;not null;check:check_user_library_grants_access_level,access_level IN ('view','edit')"`
	CreatedAt   sqliteTimestamp `gorm:"not null"`
}

func (userLibraryGrantModel) TableName() string { return "user_library_grants" }

type taskModel struct {
	ID             int64            `gorm:"primaryKey;autoIncrement"`
	Name           string           `gorm:"not null"`
	Type           string           `gorm:"not null"`
	PayloadVersion int              `gorm:"not null"`
	PayloadJSON    string           `gorm:"not null;default:'{}'"`
	CronExpression string           `gorm:"not null"`
	Timezone       string           `gorm:"not null"`
	Enabled        bool             `gorm:"not null;default:false;index:idx_tasks_due,priority:1,where:deleted_at IS NULL"`
	NextRunAt      *sqliteTimestamp `gorm:"index:idx_tasks_due,priority:2,where:deleted_at IS NULL"`
	CreatedBy      string           `gorm:"not null;default:''"`
	CreatedAt      sqliteTimestamp  `gorm:"not null"`
	UpdatedAt      sqliteTimestamp  `gorm:"not null"`
	DeletedAt      *sqliteTimestamp
}

func (taskModel) TableName() string { return "tasks" }

type taskRunModel struct {
	ID                  int64           `gorm:"primaryKey;autoIncrement;index:idx_task_runs_queue,priority:3;index:idx_task_runs_history,priority:2,sort:desc"`
	ScheduleID          *int64          `gorm:"index:idx_task_runs_schedule,priority:1;uniqueIndex:idx_task_runs_one_queued_schedule,where:schedule_id IS NOT NULL AND status = 'queued';uniqueIndex:idx_task_runs_one_running_schedule,where:schedule_id IS NOT NULL AND status = 'running'"`
	Type                string          `gorm:"not null"`
	PayloadVersion      int             `gorm:"not null"`
	PayloadJSON         string          `gorm:"not null;default:'{}'"`
	ResultJSON          string          `gorm:"not null;default:''"`
	Trigger             string          `gorm:"not null"`
	Status              string          `gorm:"not null;index:idx_task_runs_queue,priority:1"`
	ProgressStage       string          `gorm:"not null;default:''"`
	ProgressPercent     int             `gorm:"not null;default:0"`
	ProgressDirectories int             `gorm:"not null;default:0"`
	ProgressFiles       int             `gorm:"not null;default:0"`
	Attempt             int             `gorm:"not null;default:0"`
	MaxAttempts         int             `gorm:"not null;default:3"`
	AvailableAt         sqliteTimestamp `gorm:"not null;index:idx_task_runs_queue,priority:2"`
	ScheduledFor        *sqliteTimestamp
	StartedAt           *sqliteTimestamp
	FinishedAt          *sqliteTimestamp `gorm:"index:idx_task_runs_finished,where:finished_at IS NOT NULL"`
	ErrorCode           string           `gorm:"not null;default:''"`
	ErrorMessage        string           `gorm:"not null;default:''"`
	ErrorStatus         int              `gorm:"not null;default:0"`
	LeaseOwner          string           `gorm:"not null;default:''"`
	LeaseExpiresAt      *sqliteTimestamp
	CancelRequestedAt   *sqliteTimestamp
	RetryOfRunID        *int64
	CreatedBy           string          `gorm:"not null;default:''"`
	CreatedAt           sqliteTimestamp `gorm:"not null;index:idx_task_runs_history,priority:1,sort:desc;index:idx_task_runs_schedule,priority:2,sort:desc"`
	UpdatedAt           sqliteTimestamp `gorm:"not null"`

	Schedule *taskModel    `gorm:"foreignKey:ScheduleID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:NO ACTION"`
	RetryOf  *taskRunModel `gorm:"foreignKey:RetryOfRunID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:SET NULL"`
}

func (taskRunModel) TableName() string { return "task_runs" }

type taskRunItemModel struct {
	ID                 int64  `gorm:"primaryKey;autoIncrement;index:idx_task_run_items_status,priority:4;index:idx_task_run_items_action,priority:4"`
	RunID              int64  `gorm:"not null;uniqueIndex:idx_task_run_items_position,priority:1;index:idx_task_run_items_status,priority:1;index:idx_task_run_items_action,priority:1"`
	Position           int    `gorm:"not null;check:check_task_run_items_position,position >= 0;uniqueIndex:idx_task_run_items_position,priority:2;index:idx_task_run_items_status,priority:3;index:idx_task_run_items_action,priority:3"`
	RelativePath       string `gorm:"not null"`
	ObjectKind         string `gorm:"not null;check:check_task_run_items_object_kind,object_kind IN ('file','directory')"`
	Action             string `gorm:"not null;check:check_task_run_items_action,action IN ('none','mkdir','copy','move','delete');index:idx_task_run_items_action,priority:2"`
	SourcePath         string `gorm:"not null;default:''"`
	DestinationPath    string `gorm:"not null;default:''"`
	SizeBytes          int64  `gorm:"not null;default:0;check:check_task_run_items_size,size_bytes >= 0"`
	Status             string `gorm:"not null;check:check_task_run_items_status,status IN ('pending','running','succeeded','failed','skipped','canceled');index:idx_task_run_items_status,priority:2"`
	ProgressPercent    int    `gorm:"not null;default:0;check:check_task_run_items_progress,progress_percent BETWEEN 0 AND 100"`
	UpstreamTaskID     string `gorm:"not null;default:''"`
	UpstreamTaskState  string `gorm:"not null;default:''"`
	UpstreamTaskStatus string `gorm:"not null;default:''"`
	ErrorMessage       string `gorm:"not null;default:''"`
	StartedAt          *sqliteTimestamp
	FinishedAt         *sqliteTimestamp
	CreatedAt          sqliteTimestamp `gorm:"not null"`
	UpdatedAt          sqliteTimestamp `gorm:"not null"`

	Run *taskRunModel `gorm:"foreignKey:RunID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (taskRunItemModel) TableName() string { return "task_run_items" }

type mediaLibraryModel struct {
	ID                        int64  `gorm:"primaryKey;autoIncrement"`
	Name                      string `gorm:"not null"`
	ContentType               string `gorm:"not null;check:check_media_libraries_content_type,content_type IN ('movies','tvshows','music')"`
	SortOrder                 int    `gorm:"not null;default:0;index:idx_media_libraries_sort_order"`
	RootPath                  string `gorm:"not null;uniqueIndex"`
	VideoScreenshotEnabled    bool   `gorm:"not null;default:false"`
	VideoScreenshotPercent    int    `gorm:"not null;default:10"`
	PlaybackCompletionPercent int    `gorm:"not null;default:90"`
	HidePeopleWithoutImages   bool   `gorm:"not null;default:false"`
	ScanGeneration            int64  `gorm:"not null;default:0"`
	LastScannedAt             *sqliteTimestamp
	CreatedAt                 sqliteTimestamp `gorm:"not null"`
	UpdatedAt                 sqliteTimestamp `gorm:"not null"`

	Roots []mediaLibraryRootModel `gorm:"foreignKey:LibraryID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaLibraryModel) TableName() string { return "media_libraries" }

type mediaLibraryRootModel struct {
	ID        int64           `gorm:"primaryKey;autoIncrement"`
	LibraryID int64           `gorm:"not null;uniqueIndex:idx_media_library_roots_library_position,priority:1;index:idx_media_library_roots_library"`
	Position  int             `gorm:"not null;uniqueIndex:idx_media_library_roots_library_position,priority:2"`
	RootPath  string          `gorm:"not null;uniqueIndex:idx_media_library_roots_path"`
	CreatedAt sqliteTimestamp `gorm:"not null"`
	UpdatedAt sqliteTimestamp `gorm:"not null"`

	Library *mediaLibraryModel `gorm:"foreignKey:LibraryID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaLibraryRootModel) TableName() string { return "media_library_roots" }

type mediaItemModel struct {
	MetadataPriorityKey    string `gorm:"not null;default:''"`
	ID                     int64  `gorm:"primaryKey;autoIncrement;index:idx_media_items_library_parent,priority:4;index:idx_media_items_kind,priority:3"`
	LibraryID              int64  `gorm:"not null;uniqueIndex:idx_media_items_identity,priority:1;index:idx_media_items_library_parent,priority:1;index:idx_media_items_generation,priority:1"`
	ParentID               *int64 `gorm:"index:idx_media_items_library_parent,priority:2;index:idx_media_items_parent_kind_created,priority:1"`
	Kind                   string `gorm:"not null;check:check_media_items_kind,kind IN ('movie','series','season','episode','music_artist','music_album','audio');uniqueIndex:idx_media_items_identity,priority:3;index:idx_media_items_kind,priority:1;index:idx_media_items_parent_kind_created,priority:2"`
	Path                   string `gorm:"not null;uniqueIndex:idx_media_items_identity,priority:2"`
	MediaPath              string `gorm:"not null;default:''"`
	Name                   string `gorm:"not null"`
	SortName               string `gorm:"not null;default:''"`
	SortKey                string `gorm:"not null;default:'';index:idx_media_items_library_parent,priority:3;index:idx_media_items_kind,priority:2"`
	Container              string `gorm:"not null;default:''"`
	SizeBytes              int64  `gorm:"not null;default:0;check:check_media_items_size,size_bytes >= 0"`
	ModifiedAt             *sqliteTimestamp
	TVDBID                 *int64 `gorm:"column:tvdb_id"`
	MetadataSource         string `gorm:"not null;default:'auto'"`
	ResolvedMetadataSource string `gorm:"not null;default:''"`
	TMDBID                 *int64 `gorm:"column:tmdb_id"`
	IMDbID                 string `gorm:"column:imdb_id;not null;default:''"`
	GenresJSON             string `gorm:"not null;default:'[]'"`
	SeasonNumber           *int
	EpisodeNumber          *int
	ProductionYear         *int
	PremiereDate           string  `gorm:"not null;default:''"`
	Overview               string  `gorm:"not null;default:''"`
	CommunityRating        float64 `gorm:"not null;default:0"`
	RuntimeTicks           int64   `gorm:"not null;default:0"`
	ImageURL               string  `gorm:"not null;default:''"`
	BackdropURL            string  `gorm:"not null;default:''"`
	Album                  string  `gorm:"not null;default:''"`
	AlbumArtist            string  `gorm:"not null;default:''"`
	ArtistsJSON            string  `gorm:"not null;default:'[]'"`
	TrackNumber            *int
	DiscNumber             *int
	MusicSource            string          `gorm:"not null;default:''"`
	MusicTrackID           string          `gorm:"not null;default:''"`
	MusicPictureID         string          `gorm:"not null;default:''"`
	MusicLyricID           string          `gorm:"not null;default:''"`
	MusicMetadataKey       string          `gorm:"not null;default:''"`
	Lyrics                 string          `gorm:"not null;default:''"`
	TranslatedLyrics       string          `gorm:"not null;default:''"`
	MetadataLocked         bool            `gorm:"not null;default:false"`
	MetadataLocalized      bool            `gorm:"not null;default:false;index:idx_media_items_localized"`
	ScanGeneration         int64           `gorm:"not null;default:0;index:idx_media_items_generation,priority:2"`
	CreatedAt              sqliteTimestamp `gorm:"not null;index:idx_media_items_parent_kind_created,priority:3"`
	UpdatedAt              sqliteTimestamp `gorm:"not null"`

	Library *mediaLibraryModel `gorm:"foreignKey:LibraryID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
	Parent  *mediaItemModel    `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaItemModel) TableName() string { return "media_items" }

type mediaItemImageModel struct {
	ItemID                 int64           `gorm:"primaryKey;autoIncrement:false;index:idx_media_item_images_item"`
	ImageType              string          `gorm:"primaryKey;autoIncrement:false;check:check_media_item_images_type,image_type IN ('primary','backdrop')"`
	SourceType             string          `gorm:"not null;check:check_media_item_images_source_type,source_type IN ('remote','upload','auto_screenshot','manual_screenshot')"`
	SourceURL              string          `gorm:"not null;default:''"`
	SourceVideoFingerprint string          `gorm:"not null;default:''"`
	RelativePath           string          `gorm:"not null"`
	MIMEType               string          `gorm:"not null"`
	SizeBytes              int64           `gorm:"not null;default:0"`
	Width                  int             `gorm:"not null;default:0"`
	Height                 int             `gorm:"not null;default:0"`
	SHA256                 string          `gorm:"not null;index:idx_media_item_images_sha256"`
	UpdatedAt              sqliteTimestamp `gorm:"not null"`

	Item *mediaItemModel `gorm:"foreignKey:ItemID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaItemImageModel) TableName() string { return "media_item_images" }

type mediaPersonModel struct {
	Source              string          `gorm:"not null;default:'tmdb'"`
	ExternalID          int64           `gorm:"not null;default:0"`
	ID                  int64           `gorm:"primaryKey;autoIncrement:false"`
	Name                string          `gorm:"not null"`
	ProfileSourceURL    string          `gorm:"not null;default:''"`
	ProfileRelativePath string          `gorm:"not null;default:''"`
	ProfileMIMEType     string          `gorm:"not null;default:''"`
	ProfileSizeBytes    int64           `gorm:"not null;default:0"`
	ProfileWidth        int             `gorm:"not null;default:0"`
	ProfileHeight       int             `gorm:"not null;default:0"`
	ProfileSHA256       string          `gorm:"not null;default:'';index:idx_media_people_profile_sha256"`
	UpdatedAt           sqliteTimestamp `gorm:"not null"`
}

func (mediaPersonModel) TableName() string { return "media_people" }

type mediaItemPersonModel struct {
	ItemID      int64  `gorm:"primaryKey;autoIncrement:false;index:idx_media_item_people_item_order,priority:1"`
	PersonID    int64  `gorm:"primaryKey;autoIncrement:false;index:idx_media_item_people_person"`
	Type        string `gorm:"not null"`
	Role        string `gorm:"not null;default:''"`
	DisplayRole string `gorm:"not null;default:''"`
	SortOrder   int    `gorm:"not null;default:0;index:idx_media_item_people_item_order,priority:2"`

	Item   *mediaItemModel   `gorm:"foreignKey:ItemID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
	Person *mediaPersonModel `gorm:"foreignKey:PersonID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaItemPersonModel) TableName() string { return "media_item_people" }

type mediaUserDataModel struct {
	HideFromResume        bool  `gorm:"not null;default:false"`
	UserID                int64 `gorm:"primaryKey;autoIncrement:false;index:idx_media_user_data_user"`
	ItemID                int64 `gorm:"primaryKey;autoIncrement:false"`
	PlaybackPositionTicks int64 `gorm:"not null;default:0;check:check_media_user_data_position,playback_position_ticks >= 0"`
	Played                bool  `gorm:"not null;default:false"`
	PlayCount             int   `gorm:"not null;default:0;check:check_media_user_data_play_count,play_count >= 0"`
	Favorite              bool  `gorm:"not null;default:false"`
	LastPlayedAt          *sqliteTimestamp
	UpdatedAt             sqliteTimestamp `gorm:"not null"`

	Item *mediaItemModel `gorm:"foreignKey:ItemID;references:ID;constraint:OnUpdate:NO ACTION,OnDelete:CASCADE"`
}

func (mediaUserDataModel) TableName() string { return "media_user_data" }

// Provider identities never share the vendor numeric namespace. Existing TMDB
// local IDs remain untouched; allocated provider IDs occupy a separate range.
type mediaPersonIdentityModel struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	Source     string `gorm:"not null;uniqueIndex:idx_media_person_source"`
	ExternalID int64  `gorm:"not null;uniqueIndex:idx_media_person_source"`
	LocalID    int64  `gorm:"not null;uniqueIndex"`
}

func (mediaPersonIdentityModel) TableName() string { return "media_person_identities" }
