import { OpenListObject } from "@/shared/api/manager"

const supportedAudioExtensions = new Set([
  "aac",
  "aif",
  "aifc",
  "aiff",
  "ape",
  "flac",
  "m4a",
  "mp3",
  "ogg",
  "opus",
  "wav",
  "wma",
  "wv",
])

export function getFilenameExtension(name: string) {
  const lastDot = name.lastIndexOf(".")
  if (lastDot <= 0 || lastDot === name.length - 1) {
    return ""
  }
  return name.slice(lastDot + 1).toLowerCase()
}

export function supportsMusicMetadata(obj: OpenListObject) {
  return !obj.is_dir && supportedAudioExtensions.has(getFilenameExtension(obj.name))
}

export function getInitialMusicQuery(name: string) {
  const extension = getFilenameExtension(name)
  const withoutExtension = extension
    ? name.slice(0, -(extension.length + 1))
    : name
  return withoutExtension.replace(/[._]+/g, " ").trim()
}
