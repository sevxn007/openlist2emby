export type MetadataSource = "tmdb" | "tvdb"
export type MetadataSourceMode = "auto" | MetadataSource
export interface MetadataRefreshRequest { tmdb_id?: number; tvdb_id?: number; metadata_source?: MetadataSourceMode }

export interface ManagerEnvelope<T> {
  code: number
  message: string
  data: T
}

export interface ManagerStatus {
	setup_required: boolean
	openlist_base_url?: string
	openlist_admin_key_set?: boolean
	media_library_enabled?: boolean
	emby_port?: number
}

export interface SettingsResponse {
  tvdb_api_key_set?: boolean
  tvdb_pin_set?: boolean
  tvdb_api_key?: string
  tvdb_pin?: string
  metadata_priority?: MetadataSource[]

  openlist_base_url: string
  openlist_admin_key_set: boolean
  openlist_admin_key: string
	tmdb_api_key_set: boolean
	tmdb_api_key: string
	media_library_enabled: boolean
	emby_port: number
	media_metadata_path?: string
}

export interface OpenListSettingsPayload {
  openlist_base_url: string
  openlist_admin_key: string
}

export interface SetupPayload extends OpenListSettingsPayload {
 otp_code?: string
  username: string
  password: string
}

export interface TestSettingsConnectionResponse {
  openlist_base_url: string
  username: string
}

export interface TMDBSettingsPayload {
  tvdb_api_key?: string
  tvdb_pin?: string
  clear_tvdb_api_key?: boolean
  clear_tvdb_pin?: boolean
  metadata_priority?: MetadataSource[]

	tmdb_api_key?: string
}

export interface MediaLibrarySettingsPayload {
	media_library_enabled?: boolean
	emby_port?: number
}

export type MediaLibraryContentType = "movies" | "tvshows" | "music"

export interface MediaLibrary {
	id: number
	name: string
	content_type: MediaLibraryContentType
	sort_order: number
	video_screenshot_enabled: boolean
	video_screenshot_percent: number
	playback_completion_percent: number
	hide_people_without_images: boolean
	root_path?: string
	root_paths: string[]
	last_scanned_at?: string
	created_at: string
	updated_at: string
}

export interface MediaLibraryInput {
	name: string
	content_type: MediaLibraryContentType
	root_paths: string[]
	video_screenshot_enabled?: boolean
	video_screenshot_percent?: number
	playback_completion_percent?: number
	hide_people_without_images?: boolean
}

export interface MediaLibraryScanScheduleInput {
	cron_expression: string
	timezone: string
	enabled: boolean
}

export interface MediaScanScheduleParameters {
	library_id: number
	library_name?: string
}

export type MediaLibraryItemKind =
	| "movie"
	| "series"
	| "season"
	| "episode"
	| "music_artist"
	| "music_album"
	| "audio"

export interface MediaLibraryUserData {
	hide_from_resume?: boolean
	playback_position_ticks: number
	played: boolean
	play_count: number
	favorite: boolean
	last_played_at?: string
}

export interface MediaLibraryMetadataBinding {
	source_type: "tmdb_movie"
	source_id: number
}

export interface MediaLibraryItem {
 tvdb_id?: number
 metadata_source?: MetadataSourceMode
 resolved_metadata_source?: MetadataSource

	metadata_binding?: MediaLibraryMetadataBinding
	series_id?: number
	series_name?: string
	id: number
	library_id: number
	parent_id?: number
	kind: MediaLibraryItemKind
	path: string
	media_path?: string
	name: string
	sort_name: string
	container?: string
	size_bytes: number
	modified_at?: string
	tmdb_id?: number
	imdb_id?: string
	genres?: string[]
	season_number?: number
	episode_number?: number
	production_year?: number
	premiere_date?: string
	overview?: string
	community_rating?: number
	runtime_ticks?: number
	image_url?: string
	backdrop_url?: string
	image_source_url?: string
	backdrop_source_url?: string
	album?: string
	album_artist?: string
	artists?: string[]
	lyrics?: string
	translated_lyrics?: string
	track_number?: number
	disc_number?: number
	user_data: MediaLibraryUserData
	created_at: string
	updated_at: string
}

export type MediaLibraryContinueWatchingSource = "resume" | "next_up"

export interface MediaLibraryContinueWatchingItem {
	item: MediaLibraryItem
	source: MediaLibraryContinueWatchingSource
}

export interface MediaLibraryContinueWatchingPage {
	items: MediaLibraryContinueWatchingItem[]
	total: number
}

export interface MediaLibraryPerson {
	id: number
	name: string
	profile_url?: string
	type: string
	role?: string
	display_role?: string
	sort_order: number
}

export interface MediaLibraryPeoplePage {
	items: MediaLibraryPerson[]
	total: number
}

export interface MediaLibraryPage {
	items: MediaLibrary[]
	total: number
}

export interface MediaLibraryItemPage {
	items: MediaLibraryItem[]
	total: number
}

export interface MediaLibraryItemFilter {
	libraryId?: number
	parentId?: number | "root"
	kinds?: MediaLibraryItemKind[]
	search?: string
	recursive?: boolean
	latest?: boolean
	resume?: boolean
	personId?: number
	sortBy?: string
	sortOrder?: "ascending" | "descending"
	start?: number
	limit?: number
}

export interface MediaLibraryPlaybackUpdate {
	position_ticks: number
	stopped: boolean
}

export interface MediaLibraryMetadataInput {
 tvdb_id?: number | null
 metadata_source?: MetadataSourceMode

	name: string
	sort_name: string
	tmdb_id: number | null
	imdb_id: string
	genres: string[]
	season_number: number | null
	episode_number: number | null
	production_year: number | null
	premiere_date: string
	overview: string
	community_rating: number
	runtime_ticks: number
	image_url?: string
	backdrop_url?: string
	image_source_url?: string
	backdrop_source_url?: string
	album: string
	album_artist: string
	artists: string[]
	track_number: number | null
	disc_number: number | null
	lyrics: string
	translated_lyrics: string
}

export interface TestTMDBConnectionResponse {
  connected: boolean
}

export interface MediaSearchResult {
 source?: MetadataSource
  id: number
  media_type: "movie" | "tv"
  title: string
  name?: string
  original_title?: string
  overview: string
  poster_path?: string
  poster_url?: string
  release_date?: string
  first_air_date?: string
  vote_average: number
  genre_ids?: number[]
}

export interface MediaSeason {
 source?: MetadataSource
  id: number
  air_date?: string
  episode_count: number
  name: string
  overview?: string
  poster_path?: string
  poster_url?: string
  season_number: number
  vote_average: number
}

export interface MediaEpisode {
 source?: MetadataSource
  id: number
  air_date?: string
  episode_number: number
  name: string
  overview?: string
  runtime?: number
  season_number: number
  still_path?: string
  still_url?: string
  vote_average: number
  vote_count: number
}

export interface MediaDetail {
 source?: MetadataSource
  id: number
  media_type: "movie" | "tv"
  title?: string
  name?: string
  original_title?: string
  overview?: string
  poster_path?: string
  poster_url?: string
  release_date?: string
  first_air_date?: string
  vote_average: number
  number_of_seasons?: number
  number_of_episodes?: number
  season_number?: number
  seasons?: MediaSeason[]
  episodes?: MediaEpisode[]
}

export interface MediaSearchResponse {
  results: MediaSearchResult[]
}

export interface MusicTrack {
  id: string
  name: string
  artists: string[]
  album: string
  picture_id: string
  lyric_id: string
  source: string
  duration_ms?: number
  track_number?: number
  disc_number?: number
}

export type MusicSource = "netease" | "qmusic" | "kuwo"

export interface MusicSearchResponse {
  results: MusicTrack[]
}

export interface MusicDetail {
  cover_url?: string
  lyrics?: string
  translated_lyrics?: string
}

export interface EmbedMusicPayload {
	item_id: number
  source: string
  picture_id: string
  lyric_id: string
  embed_cover: boolean
  embed_lyrics: boolean
}

export interface EmbedMusicResponse {
  embedded_cover: boolean
  embedded_lyrics: boolean
}

export const musicEmbedStages = [
  "preparing",
  "fetching_metadata",
  "downloading",
  "embedding",
  "uploading",
  "completed",
] as const

export type MusicEmbedStage = (typeof musicEmbedStages)[number]

export interface MusicEmbedProgress {
  stage: MusicEmbedStage
  percent: number
}

export interface LoginPayload {
 otp_code?: string
  username: string
  password: string
}

export interface CurrentUser {
	id: number
  username: string
	role: LocalUserRole
	expires_at: string
	is_admin: boolean
	editable_library_ids: number[]
}

export type LocalUserRole = "admin" | "media"

export type MediaLibraryAccess = "view" | "edit"

export interface MediaLibraryGrant {
	library_id: number
	access: MediaLibraryAccess
}

export interface ManagerUser {
	openlist_user_id: number
	upstream_status: "active" | "disabled" | "unavailable"
	id: number
	username: string
	role: LocalUserRole
	enabled: boolean
	library_grants: MediaLibraryGrant[]
	created_at: string
	updated_at: string
}

export interface ManagerUserPage {
	items: ManagerUser[]
	total: number
}

export interface GrantIdentityPayload {
	openlist_user_id: number
	role: LocalUserRole
	enabled: boolean
	library_grants: MediaLibraryGrant[]
}
export interface OpenListIdentity {
	id: number
	username: string
	role: number
	disabled: boolean
	otp: boolean
}
export interface OpenListIdentityPage {
	content: OpenListIdentity[]
	total: number
}
export interface UpdateManagerUserPayload {
	role?: LocalUserRole
	enabled?: boolean
	library_grants?: MediaLibraryGrant[]
}

export const MUSIC_EMBED_TASK_TYPE = "music.embed_metadata"

export type TaskRunStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"

export type TaskTrigger = "manual" | "schedule" | "run_now" | "retry"

export interface TaskType {
  type: string
  payload_version: number
  manual: boolean
  schedulable: boolean
  cancelable: boolean
}

export interface TaskProgress {
  stage: string
  percent: number
  directories_scanned?: number
  files_scanned?: number
}

export interface TaskRunError {
  code: string
  message: string
}

export interface TaskRun {
  id: number
  schedule_id?: number
  type: string
  payload_version: number
  subject?: unknown
  trigger: TaskTrigger
  status: TaskRunStatus
  progress: TaskProgress
  attempt: number
  max_attempts: number
  available_at: string
  scheduled_for?: string
  started_at?: string
  finished_at?: string
  cancel_requested: boolean
  retry_of_run_id?: number
  created_by: string
  created_at: string
  updated_at: string
  error?: TaskRunError
  result?: unknown
}

export interface TaskRunPage {
  items: TaskRun[]
  total: number
  page: number
  per_page: number
}

export interface TaskRunFilter {
  statuses?: readonly TaskRunStatus[]
  type?: string
  page?: number
  perPage?: number
}

export type OpenListTaskKind =
  | "upload"
  | "copy"
  | "move"
  | "offline_download"
  | "offline_download_transfer"
  | "decompress"
  | "decompress_upload"

export interface OpenListTask {
  id: string
  kind: OpenListTaskKind
  name: string
  state: number
  status: string
  progress: number
  start_time?: string
  end_time?: string
  total_bytes: number
  error?: string
}

export interface CreateTaskRunPayload {
  type: string
  payload: unknown
}

export interface TaskSchedule {
  id: number
  name: string
  type: string
  payload_version: number
  parameters?: unknown
  cron_expression: string
  timezone: string
  enabled: boolean
  next_run_at?: string
  created_by: string
  created_at: string
  updated_at: string
  last_run?: TaskRun
}

export interface TaskSchedulePage {
  items: TaskSchedule[]
  total: number
}

export interface TaskSchedulePayload {
  name: string
  type: string
  cron_expression: string
  timezone: string
  enabled: boolean
  payload: unknown
}

export const MEDIA_SCAN_TASK_TYPE = "media.scan"

export type TaskRunItemStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "canceled"

export interface TaskRunItem {
  sequence: number
  relative_path: string
  object_type: string
  action: string
  source_path?: string
  target_path?: string
  size: number
  id: number
  run_id: number
  status: TaskRunItemStatus
  percent: number
  upstream_task_id?: string
  upstream_task_state?: string
  upstream_task_status?: string
  error?: TaskRunError
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export interface TaskRunItemSummary {
  statuses: Partial<Record<TaskRunItemStatus, number>>
  actions: Record<string, number>
}

export interface TaskRunItemPage {
  items: TaskRunItem[]
  total: number
  page: number
  per_page: number
  summary: TaskRunItemSummary
}

export interface TaskRunItemFilter {
  status?: TaskRunItemStatus
  action?: string
  page?: number
  perPage?: number
}

export interface OpenListObject {
  name: string
  size: number
  is_dir: boolean
  modified: string
  created: string
  sign: string
  thumb: string
  type: number
  hashinfo: string
  hash_info?: Record<string, string>
}

export interface FileListResponse {
  content: OpenListObject[]
  total: number
  readme: string
  header: string
  write: boolean
  write_content_bypass: boolean
  provider: string
  direct_upload_tools?: string[]
}

export interface ListFilesOptions {
  path: string
  refresh?: boolean
}

export interface CreateFolderPayload {
  path: string
}

export interface RenamePayload {
  path: string
  name: string
  overwrite?: boolean
}

export interface MoveCopyPayload {
  src_dir: string
  dst_dir: string
  names: string[]
  overwrite?: boolean
  skip_existing?: boolean
  merge?: boolean
}

export interface RemovePayload {
  dir: string
  names: string[]
}

export interface RenameObject {
  src_name: string
  new_name: string
}

export interface BatchRenamePayload {
  src_dir: string
  rename_objects: RenameObject[]
}

export class ManagerApiError extends Error {
  readonly code: number

  constructor(message: string, code: number) {
    super(message)
    this.name = "ManagerApiError"
    this.code = code
  }
}

export const MANAGER_SESSION_EXPIRED_EVENT = "manager-session-expired"

async function requestManager<T>(
  pathname: string,
  init: RequestInit = {},
): Promise<T> {
  const isMultipart = typeof FormData !== "undefined" && init.body instanceof FormData
  const response = await fetch(pathname, {
    ...init,
    credentials: "include",
    headers: isMultipart
      ? init.headers
      : {
          "Content-Type": "application/json",
          ...(init.headers ?? {}),
        },
  })

  return unwrapManagerResponse<T>(response)
}

async function unwrapManagerResponse<T>(response: Response): Promise<T> {
  const envelope = (await response.json()) as ManagerEnvelope<T>
  if (!response.ok || envelope.code < 200 || envelope.code >= 300) {
    const code = envelope.code || response.status
    if (code === 401 && typeof window !== "undefined") {
      window.dispatchEvent(new Event(MANAGER_SESSION_EXPIRED_EVENT))
    }
    throw new ManagerApiError(
      envelope.message || `Request failed with HTTP ${response.status}`,
      code,
    )
  }

  return envelope.data
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null
}

function parseMusicEmbedProgress(value: unknown): MusicEmbedProgress | null {
  if (
    !isRecord(value) ||
    typeof value.stage !== "string" ||
    !musicEmbedStages.includes(value.stage as MusicEmbedStage) ||
    typeof value.percent !== "number"
  ) {
    return null
  }
  return {
    stage: value.stage as MusicEmbedStage,
    percent: Math.min(Math.max(Math.round(value.percent), 0), 100),
  }
}

function parseMusicEmbedResult(value: unknown): EmbedMusicResponse | null {
  if (
    !isRecord(value) ||
    typeof value.embedded_cover !== "boolean" ||
    typeof value.embedded_lyrics !== "boolean"
  ) {
    return null
  }
  return {
    embedded_cover: value.embedded_cover,
    embedded_lyrics: value.embedded_lyrics,
  }
}

function parseMusicEmbedError(value: unknown): ManagerApiError {
  if (
    isRecord(value) &&
    typeof value.code === "number" &&
    typeof value.message === "string"
  ) {
    return new ManagerApiError(value.message, value.code)
  }
  return new ManagerApiError("", 502)
}

async function readMusicEmbedStream(
  body: ReadableStream<Uint8Array>,
  onProgress: (progress: MusicEmbedProgress) => void,
): Promise<EmbedMusicResponse> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  let result: EmbedMusicResponse | null = null

  function dispatch(block: string) {
    let event = "message"
    const data: string[] = []
    for (const line of block.split("\n")) {
      if (line.startsWith("event:")) {
        event = line.slice(6).trim()
      } else if (line.startsWith("data:")) {
        data.push(line.slice(5).trimStart())
      }
    }
    if (data.length === 0) {
      return
    }

    let value: unknown
    try {
      value = JSON.parse(data.join("\n"))
    } catch {
      throw new ManagerApiError("", 502)
    }
    if (event === "progress") {
      const progress = parseMusicEmbedProgress(value)
      if (!progress) {
        throw new ManagerApiError("", 502)
      }
      onProgress(progress)
    } else if (event === "complete") {
      result = parseMusicEmbedResult(value)
      if (!result) {
        throw new ManagerApiError("", 502)
      }
    } else if (event === "error") {
      throw parseMusicEmbedError(value)
    }
  }

  function drain() {
    buffer = buffer.replace(/\r\n/g, "\n")
    let boundary = buffer.indexOf("\n\n")
    while (boundary >= 0) {
      dispatch(buffer.slice(0, boundary))
      buffer = buffer.slice(boundary + 2)
      boundary = buffer.indexOf("\n\n")
    }
  }

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        buffer += decoder.decode()
        if (buffer.trim()) {
          dispatch(buffer.trim())
        }
        break
      }
      buffer += decoder.decode(value, { stream: true })
      drain()
    }
  } finally {
    reader.releaseLock()
  }

  if (!result) {
    throw new ManagerApiError("", 502)
  }
  return result
}

export function getStatus() {
  return requestManager<ManagerStatus>("/api/manager/status")
}

export function setupManager(payload: SetupPayload) {
  return requestManager<ManagerStatus>("/api/manager/setup", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function login(payload: LoginPayload) {
  return requestManager<CurrentUser>("/api/manager/auth/login", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function logout() {
  return requestManager<null>("/api/manager/auth/logout", {
    method: "POST",
    body: JSON.stringify({}),
  })
}

export function getCurrentUser() {
  return requestManager<CurrentUser>("/api/manager/auth/me")
}

export function listManagerUsers() {
	return requestManager<ManagerUserPage>("/api/manager/users")
}

export function grantIdentity(payload: GrantIdentityPayload) {
	return requestManager<ManagerUser>("/api/manager/user-authorizations", {
		method: "POST",
		body: JSON.stringify(payload),
	})
}

export function updateManagerUser(id: number, payload: UpdateManagerUserPayload) {
	return requestManager<ManagerUser>(`/api/manager/users/${id}`, {
		method: "PUT",
		body: JSON.stringify(payload),
	})
}

export function listOpenListUsers(page = 1) {
 return requestManager<OpenListIdentityPage>(`/api/manager/openlist/users?page=${page}&per_page=50`)
}

export function getSettings() {
  return requestManager<SettingsResponse>("/api/manager/settings")
}

export function updateSettings(
	payload: OpenListSettingsPayload &
	TMDBSettingsPayload &
	MediaLibrarySettingsPayload,
) {
  return requestManager<SettingsResponse>("/api/manager/settings", {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function listMediaLibraries() {
	return requestManager<MediaLibraryPage>("/api/manager/media-libraries")
}

export function listAllMediaLibraries() {
	return requestManager<MediaLibraryPage>(
		"/api/manager/media-libraries?include_hidden=true",
	)
}

export function createMediaLibrary(payload: MediaLibraryInput) {
	return requestManager<MediaLibrary>("/api/manager/media-libraries", {
		method: "POST",
		body: JSON.stringify(payload),
	})
}

export function updateMediaLibrary(id: number, payload: MediaLibraryInput) {
	return requestManager<MediaLibrary>(`/api/manager/media-libraries/${id}`, {
		method: "PUT",
		body: JSON.stringify(payload),
	})
}

export function reorderMediaLibraries(libraryIDs: number[]) {
	return requestManager<{ reordered: boolean }>("/api/manager/media-libraries/order", {
		method: "PUT",
		body: JSON.stringify({ library_ids: libraryIDs }),
	})
}

export function deleteMediaLibrary(id: number) {
	return requestManager<{ deleted: boolean }>(
		`/api/manager/media-libraries/${id}`,
		{ method: "DELETE" },
	)
}

export function scanMediaLibrary(id: number) {
	return requestManager<TaskRun>(`/api/manager/media-libraries/${id}/scan`, {
		method: "POST",
		body: JSON.stringify({}),
	})
}

export function getMediaLibraryScanSchedule(id: number) {
	return requestManager<TaskSchedule>(
		`/api/manager/media-libraries/${id}/scan-schedule`,
	)
}

export function updateMediaLibraryScanSchedule(
	id: number,
	payload: MediaLibraryScanScheduleInput,
) {
	return requestManager<TaskSchedule>(
		`/api/manager/media-libraries/${id}/scan-schedule`,
		{
			method: "PUT",
			body: JSON.stringify(payload),
		},
	)
}

export function migrateMediaMetadataStorage(path: string) {
	return requestManager<TaskRun>("/api/manager/media-library/storage/migrations", {
		method: "POST",
		body: JSON.stringify({ path }),
	})
}

export function listMediaLibraryItems(filter: MediaLibraryItemFilter = {}) {
	const params = new URLSearchParams()
	if (filter.libraryId !== undefined) params.set("library_id", String(filter.libraryId))
	if (filter.parentId !== undefined) params.set("parent_id", String(filter.parentId))
	if (filter.kinds?.length) params.set("kinds", filter.kinds.join(","))
	if (filter.search) params.set("search", filter.search)
	if (filter.recursive) params.set("recursive", "true")
	if (filter.latest) params.set("latest", "true")
	if (filter.resume) params.set("resume", "true")
	if (filter.personId !== undefined) params.set("person_id", String(filter.personId))
	if (filter.sortBy) params.set("sort_by", filter.sortBy)
	if (filter.sortOrder) params.set("sort_order", filter.sortOrder)
	if (filter.start !== undefined) params.set("start", String(filter.start))
	if (filter.limit !== undefined) params.set("limit", String(filter.limit))
	const query = params.toString()
	return requestManager<MediaLibraryItemPage>(
		`/api/manager/media-library/items${query ? `?${query}` : ""}`,
	)
}

export function getMediaLibraryItem(id: number) {
	return requestManager<MediaLibraryItem>(`/api/manager/media-library/items/${id}`)
}

export function listMediaLibraryContinueWatching(start = 0, limit = 24) {
	const params = new URLSearchParams({ start: String(start), limit: String(limit) })
	return requestManager<MediaLibraryContinueWatchingPage>(
		`/api/manager/media-library/continue-watching?${params.toString()}`,
	)
}

export function getNextMediaLibraryEpisode(id: number) {
	return requestManager<{ item: MediaLibraryItem | null }>(
		`/api/manager/media-library/items/${id}/next-episode`,
	)
}

export function listMediaLibraryItemPeople(id: number, language: string) {
	const params = new URLSearchParams({ language })
	return requestManager<MediaLibraryPeoplePage>(
		`/api/manager/media-library/items/${id}/people?${params.toString()}`,
	)
}

export function updateMediaLibraryItemMetadata(
	id: number,
	payload: MediaLibraryMetadataInput,
) {
	return requestManager<MediaLibraryItem>(
		`/api/manager/media-library/items/${id}/metadata`,
		{
			method: "PUT",
			body: JSON.stringify(payload),
		},
	)
}

export function uploadMediaLibraryItemImage(
	id: number,
	type: "primary" | "backdrop",
	file: File,
) {
	const body = new FormData()
	body.set("file", file)
	return requestManager<MediaLibraryItem>(
		`/api/manager/media-library/items/${id}/images/${type}`,
		{ method: "PUT", body },
	)
}

export function captureMediaLibraryItemPrimaryImage(id: number) {
	return requestManager<MediaLibraryItem>(
		`/api/manager/media-library/items/${id}/images/primary/capture`,
		{ method: "POST" },
	)
}

export function refreshMediaLibraryItemMetadata(
	id: number,
	request?: number | MetadataRefreshRequest,
) {
	return requestManager<MediaLibraryItem>(
		`/api/manager/media-library/items/${id}/metadata/refresh`,
		{
			method: "POST",
			body: JSON.stringify(typeof request === "object" ? request : { tmdb_id: request }),
		},
	)
}

export function getMediaLibraryStreamURL(id: number) {
	return `/api/manager/media-library/items/${id}/stream`
}

export function updateMediaLibraryPlayback(
	id: number,
	payload: MediaLibraryPlaybackUpdate,
	keepalive = false,
) {
	return requestManager<MediaLibraryUserData>(
		`/api/manager/media-library/items/${id}/playback`,
		{
			method: "PUT",
			body: JSON.stringify(payload),
			keepalive,
		},
	)
}

export function setMediaLibraryPlayed(id: number, played: boolean) {
	return requestManager<MediaLibraryUserData>(
		`/api/manager/media-library/items/${id}/played`,
		{ method: "PUT", body: JSON.stringify({ played }) },
	)
}

export function clearMediaLibraryResume(id: number) {
	return requestManager<MediaLibraryUserData>(
		`/api/manager/media-library/items/${id}/resume`,
		{ method: "DELETE" },
	)
}

export function testSettingsConnection(payload: OpenListSettingsPayload) {
  return requestManager<TestSettingsConnectionResponse>(
    "/api/manager/settings/test",
    {
      method: "POST",
      body: JSON.stringify(payload),
    },
  )
}

export function testTMDBConnection(payload: TMDBSettingsPayload & { source?: MetadataSource } = {}) {
  return requestManager<TestTMDBConnectionResponse>("/api/manager/media/test", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function searchMedia(query: string, language: string, source?: MetadataSourceMode) {
  const params = new URLSearchParams({ query, language })
  if (source) params.set("source", source)
  return requestManager<MediaSearchResponse>(
    `/api/manager/media/search?${params.toString()}`,
  )
}

export function getMediaDetail(
  type: "movie" | "tv",
  id: number,
  language: string,
  season?: number,
  source?: MetadataSource,
) {
  const params = new URLSearchParams({
    type,
    id: String(id),
    language,
  })
  if (source) params.set("source", source)
  if (season !== undefined) {
    params.set("season", String(season))
  }
  return requestManager<MediaDetail>(
    `/api/manager/media/detail?${params.toString()}`,
  )
}

export function searchMusic(query: string, source: MusicSource = "netease") {
  const params = new URLSearchParams({ query, source })
  return requestManager<MusicSearchResponse>(
    `/api/manager/music/search?${params.toString()}`,
  )
}

export function getMusicDetail(
  source: string,
  pictureId: string,
  lyricId: string,
) {
  const params = new URLSearchParams({ source })
  if (pictureId) params.set("picture_id", pictureId)
  if (lyricId) params.set("lyric_id", lyricId)
  return requestManager<MusicDetail>(
    `/api/manager/music/detail?${params.toString()}`,
  )
}

export async function embedMusic(
  payload: EmbedMusicPayload,
  onProgress: (progress: MusicEmbedProgress) => void = () => undefined,
) {
  const response = await fetch("/api/manager/music/embed", {
    method: "POST",
    credentials: "include",
    headers: {
      Accept: "text/event-stream",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  })
  if (
    !response.ok ||
    !response.headers.get("Content-Type")?.includes("text/event-stream")
  ) {
    return unwrapManagerResponse<EmbedMusicResponse>(response)
  }
  if (!response.body) {
    throw new ManagerApiError("", 502)
  }
  return readMusicEmbedStream(response.body, onProgress)
}

export function listTaskTypes() {
  return requestManager<{ items: TaskType[] }>("/api/manager/tasks/types")
}

export function listTaskRuns(filter: TaskRunFilter = {}) {
  const params = new URLSearchParams()
  if (filter.statuses?.length) {
    params.set("status", filter.statuses.join(","))
  }
  if (filter.type) {
    params.set("type", filter.type)
  }
  if (filter.page) {
    params.set("page", String(filter.page))
  }
  if (filter.perPage) {
    params.set("per_page", String(filter.perPage))
  }
  const query = params.toString()
  return requestManager<TaskRunPage>(
    `/api/manager/tasks/runs${query ? `?${query}` : ""}`,
  )
}

export function listOpenListTasks() {
  return requestManager<{ items: OpenListTask[] }>(
    "/api/manager/openlist/tasks",
  )
}

export function cancelOpenListTask(task: Pick<OpenListTask, "kind" | "id">) {
  return requestManager<{ canceled: boolean }>(
    `/api/manager/openlist/tasks/${encodeURIComponent(task.kind)}/${encodeURIComponent(task.id)}/cancel`,
    { method: "POST", body: JSON.stringify({}) },
  )
}

export function createTaskRun(payload: CreateTaskRunPayload) {
  return requestManager<TaskRun>("/api/manager/tasks/runs", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function getTaskRun(id: number) {
  return requestManager<TaskRun>(`/api/manager/tasks/runs/${id}`)
}

export function deleteTaskRun(id: number) {
  return requestManager<{ deleted: boolean }>(`/api/manager/tasks/runs/${id}`, {
    method: "DELETE",
  })
}

export function deleteCompletedTaskRuns() {
  return requestManager<{ deleted: number }>("/api/manager/tasks/runs/completed", {
    method: "DELETE",
  })
}

export function cancelTaskRun(id: number) {
  return requestManager<TaskRun>(`/api/manager/tasks/runs/${id}/cancel`, {
    method: "POST",
    body: JSON.stringify({}),
  })
}

export function retryTaskRun(id: number) {
  return requestManager<TaskRun>(`/api/manager/tasks/runs/${id}/retry`, {
    method: "POST",
    body: JSON.stringify({}),
  })
}

export function listTaskSchedules(type?: string) {
  const params = new URLSearchParams()
  if (type) {
    params.set("type", type)
  }
  const query = params.toString()
  return requestManager<TaskSchedulePage>(
    `/api/manager/tasks/schedules${query ? `?${query}` : ""}`,
  )
}

export function getTaskSchedule(id: number) {
  return requestManager<TaskSchedule>(`/api/manager/tasks/schedules/${id}`)
}

export function createTaskSchedule(payload: TaskSchedulePayload) {
  return requestManager<TaskSchedule>("/api/manager/tasks/schedules", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateTaskSchedule(id: number, payload: TaskSchedulePayload) {
  return requestManager<TaskSchedule>(`/api/manager/tasks/schedules/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function deleteTaskSchedule(id: number) {
  return requestManager<{ deleted: boolean }>(
    `/api/manager/tasks/schedules/${id}`,
    { method: "DELETE" },
  )
}

export function runTaskSchedule(id: number) {
  return requestManager<TaskRun>(`/api/manager/tasks/schedules/${id}/run`, {
    method: "POST",
    body: JSON.stringify({}),
  })
}

export function listTaskRunItems(id: number, filter: TaskRunItemFilter = {}) {
  const params = new URLSearchParams()
  if (filter.status) {
    params.set("status", filter.status)
  }
  if (filter.action) {
    params.set("action", filter.action)
  }
  if (filter.page) {
    params.set("page", String(filter.page))
  }
  if (filter.perPage) {
    params.set("per_page", String(filter.perPage))
  }
  const query = params.toString()
  return requestManager<TaskRunItemPage>(
    `/api/manager/tasks/runs/${id}/items${query ? `?${query}` : ""}`,
  )
}

export function listFiles({ path, refresh = false }: ListFilesOptions) {
  return requestManager<FileListResponse>("/api/manager/openlist/fs/list", {
    method: "POST",
    body: JSON.stringify({
      path,
      page: 1,
      per_page: 0,
      refresh,
    }),
  })
}

export function createFolder(payload: CreateFolderPayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/mkdir", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function renameFile(payload: RenamePayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/rename", {
    method: "POST",
    body: JSON.stringify({
      ...payload,
      overwrite: payload.overwrite ?? false,
    }),
  })
}

export function moveFiles(payload: MoveCopyPayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/move", {
    method: "POST",
    body: JSON.stringify({
      overwrite: false,
      skip_existing: false,
      merge: false,
      ...payload,
    }),
  })
}

export function copyFiles(payload: MoveCopyPayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/copy", {
    method: "POST",
    body: JSON.stringify({
      overwrite: false,
      skip_existing: false,
      merge: false,
      ...payload,
    }),
  })
}

export function removeFiles(payload: RemovePayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/remove", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function batchRenameFiles(payload: BatchRenamePayload) {
  return requestManager<unknown>("/api/manager/openlist/fs/batch_rename", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function buildDownloadUrl(
  parentPath: string,
  obj: OpenListObject,
  openListBaseURL?: string,
) {
  if (obj.is_dir || !obj.sign || !openListBaseURL) {
    return null
  }

  const fullPath = joinOpenListPath(parentPath, obj.name)
  const encodedPath = fullPath
    .split("/")
    .map((part) => encodeURIComponent(part))
    .join("/")

  return `${openListBaseURL.replace(/\/+$/, "")}/d${encodedPath}?sign=${encodeURIComponent(obj.sign)}`
}

export function joinOpenListPath(parentPath: string, name: string) {
  const normalizedParent = normalizeOpenListPath(parentPath)
  if (normalizedParent === "/") {
    return `/${name}`
  }
  return `${normalizedParent}/${name}`
}

export function normalizeOpenListPath(path: string) {
  const trimmed = path.trim()
  if (!trimmed || trimmed === "/") {
    return "/"
  }
  const withLeadingSlash = trimmed.startsWith("/") ? trimmed : `/${trimmed}`
  return withLeadingSlash.replace(/\/+/g, "/").replace(/\/$/, "") || "/"
}

export function setMediaLibraryResumeVisibility(id: number, hidden: boolean) {
 return requestManager<MediaLibraryUserData>(
  `/api/manager/media-library/items/${id}/resume-visibility`,
  { method: "PUT", body: JSON.stringify({ hidden }) },
 )
}

export interface MediaImageQuery {
    source: MetadataSource
    type: "movie" | "tv"
    id: number
    image_type: "primary" | "backdrop"
    season?: number
    episode?: number
}

export interface MediaImage {
    source: MetadataSource
    image_type: "primary" | "backdrop"
    url: string
    thumbnail_url: string
    width?: number
    height?: number
    language?: string
}

export interface MediaImageSelection {
    query: MediaImageQuery
    image: MediaImage
}

export function getMediaImageSources() {
    return requestManager<{ sources: MetadataSource[] }>("/api/manager/media/image-sources")
}

export function getMediaImages(query: MediaImageQuery) {
    const params = new URLSearchParams({
        source: query.source,
        type: query.type,
        id: String(query.id),
        image_type: query.image_type
    })
    if (query.season !== undefined) params.set("season", String(query.season))
    if (query.episode !== undefined) params.set("episode", String(query.episode))
    return requestManager<{ images: MediaImage[] }>(`/api/manager/media/images?${params}`)
}

export function selectMediaLibraryItemImage(id: number, selection: MediaImageSelection) {
    const {query, image} = selection
    return requestManager<MediaLibraryItem>(`/api/manager/media-library/items/${id}/images/${query.image_type}/select`, {
        method: "POST",
        body: JSON.stringify({...query, url: image.url}),
    })
}
