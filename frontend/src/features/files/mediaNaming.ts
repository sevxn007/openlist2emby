import {
  MediaEpisode,
  MediaSearchResult,
  MediaSeason,
  OpenListObject,
} from "@/shared/api/manager"

const videoExtensions = new Set([
  "mp4",
  "mkv",
  "mov",
  "avi",
  "webm",
  "flv",
  "wmv",
  "m4v",
  "mpeg",
  "mpg",
  "ts",
  "mts",
  "m2ts",
])

function getExtension(name: string) {
  const lastDot = name.lastIndexOf(".")
  if (lastDot <= 0 || lastDot === name.length - 1) {
    return ""
  }
  return name.slice(lastDot + 1).toLowerCase()
}

function getOriginalExtension(name: string, isDirectory: boolean) {
  if (isDirectory) {
    return ""
  }
  const dot = name.lastIndexOf(".")
  return dot > 0 ? name.slice(dot) : ""
}

export function supportsMediaSearch(obj: OpenListObject) {
  return obj.is_dir || videoExtensions.has(getExtension(obj.name))
}

export function isVideoFile(obj: OpenListObject) {
  return !obj.is_dir && videoExtensions.has(getExtension(obj.name))
}

export function getMediaDisplayTitle(item: MediaSearchResult) {
  return item.title || item.name || item.original_title || "-"
}

export function formatSeasonCode(seasonNumber: number) {
  return `S${String(seasonNumber).padStart(2, "0")}`
}

export function formatEpisodeCode(episodeNumber: number) {
  return `E${String(episodeNumber).padStart(2, "0")}`
}

export function buildMovieRename(
  item: MediaSearchResult,
  originalName: string,
  isDirectory: boolean,
) {
  return `${getMediaDisplayTitle(item)}${getOriginalExtension(originalName, isDirectory)}`
}

export function buildEpisodeRename(
  item: MediaSearchResult,
  season: MediaSeason,
  episode: MediaEpisode,
  originalName: string,
  isDirectory: boolean,
) {
  return [
    getMediaDisplayTitle(item),
    `${formatSeasonCode(season.season_number)}${formatEpisodeCode(episode.episode_number)}`,
    episode.name || `Episode ${episode.episode_number}`,
  ].join(" - ") + getOriginalExtension(originalName, isDirectory)
}
