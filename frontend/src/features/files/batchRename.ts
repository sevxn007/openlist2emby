import {
  MediaEpisode,
  MediaSearchResult,
  MediaSeason,
  OpenListObject,
  RenameObject,
} from "@/shared/api/manager"
import { buildEpisodeRename, isVideoFile } from "./mediaNaming"

export type BatchRenameMode = "regex" | "sequence" | "find" | "media"

export interface MediaEpisodeSelection {
  item: MediaSearchResult
  season: MediaSeason
  episodes: MediaEpisode[]
  startEpisodeNumber: number
}

export type BatchRenameConfig =
  | { mode: "regex"; pattern: string; replacement: string }
  | { mode: "sequence"; template: string; start: string; padding: number }
  | { mode: "find"; search: string; replacement: string }
  | { mode: "media"; selection: MediaEpisodeSelection | null }

export type RenamePlanErrorCode =
  | "noSelection"
  | "invalidRegex"
  | "invalidSequence"
  | "invalidName"
  | "noMatches"
  | "duplicateTarget"
  | "nameConflict"
  | "mediaVideoOnly"
  | "mediaSelectionRequired"
  | "insufficientEpisodes"

export type RenamePlanResult =
  | { ok: true; renameObjects: RenameObject[] }
  | { ok: false; code: RenamePlanErrorCode; name?: string }

const DATE_PARTS = [
  ["year", (date: Date) => date.getFullYear(), 4],
  ["month", (date: Date) => date.getMonth() + 1, 2],
  ["date", (date: Date) => date.getDate(), 2],
  ["hour", (date: Date) => date.getHours(), 2],
  ["minute", (date: Date) => date.getMinutes(), 2],
  ["second", (date: Date) => date.getSeconds(), 2],
] as const

function replaceDatePlaceholders(
  value: string,
  prefix: "created" | "modified",
  rawDate: string,
) {
  const date = new Date(rawDate)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  let result = value
  for (const [part, read, width] of DATE_PARTS) {
    result = result
      .split(`{${prefix}_${part}}`)
      .join(String(read(date)).padStart(width, "0"))
  }
  return result
}

function applyDatePlaceholders(value: string, item: OpenListObject) {
  return replaceDatePlaceholders(
    replaceDatePlaceholders(value, "created", item.created),
    "modified",
    item.modified,
  )
}

function getOriginalExtension(item: OpenListObject) {
  if (item.is_dir) {
    return ""
  }
  const dot = item.name.lastIndexOf(".")
  return dot > 0 && dot < item.name.length - 1 ? item.name.slice(dot) : ""
}

function isValidRelativeName(name: string) {
  const trimmed = name.trim()
  return Boolean(trimmed && trimmed !== "." && trimmed !== ".." && !/[\\/]/.test(name))
}

function validatePlan(
  renameObjects: RenameObject[],
  allItems: OpenListObject[],
): RenamePlanResult {
  const changed = renameObjects.filter((item) => item.src_name !== item.new_name)
  if (changed.length === 0) {
    return { ok: false, code: "noMatches" }
  }

  const targets = new Set<string>()
  const existingNames = new Set(allItems.map((item) => item.name))
  for (const item of changed) {
    if (!isValidRelativeName(item.new_name)) {
      return { ok: false, code: "invalidName", name: item.new_name }
    }
    if (targets.has(item.new_name)) {
      return { ok: false, code: "duplicateTarget", name: item.new_name }
    }
    if (existingNames.has(item.new_name) && item.new_name !== item.src_name) {
      return { ok: false, code: "nameConflict", name: item.new_name }
    }
    targets.add(item.new_name)
  }

  return { ok: true, renameObjects: changed }
}

function buildRegexPlan(
  config: Extract<BatchRenameConfig, { mode: "regex" }>,
  items: OpenListObject[],
): RenameObject[] | null {
  if (!config.pattern) {
    return null
  }
  let expression: RegExp
  try {
    expression = new RegExp(config.pattern, "g")
  } catch {
    return null
  }

  return items.flatMap((item) => {
    expression.lastIndex = 0
    if (!expression.test(item.name)) {
      return []
    }
    expression.lastIndex = 0
    const newName = applyDatePlaceholders(
      item.name.replace(expression, config.replacement),
      item,
    )
    return [{ src_name: item.name, new_name: newName }]
  })
}

function buildSequencePlan(
  config: Extract<BatchRenameConfig, { mode: "sequence" }>,
  items: OpenListObject[],
): RenameObject[] | null {
  if (
    !config.template.trim() ||
    !/^\d+$/.test(config.start) ||
    !Number.isSafeInteger(config.padding) ||
    config.padding < 0 ||
    config.padding > 255
  ) {
    return null
  }
  const start = Number(config.start)
  if (!Number.isSafeInteger(start)) {
    return null
  }
  const width = Math.max(config.padding, config.start.startsWith("0") ? config.start.length : 0)

  return items.map((item, index) => {
    const number = String(start + index).padStart(width, "0")
    const baseName = config.template.includes("{number}")
      ? config.template.split("{number}").join(number)
      : `${config.template}${number}`
    return {
      src_name: item.name,
      new_name: `${baseName}${getOriginalExtension(item)}`,
    }
  })
}

function buildFindPlan(
  config: Extract<BatchRenameConfig, { mode: "find" }>,
  items: OpenListObject[],
): RenameObject[] {
  if (!config.search) {
    return []
  }
  return items.flatMap((item) =>
    item.name.includes(config.search)
      ? [{ src_name: item.name, new_name: item.name.replace(config.search, config.replacement) }]
      : [],
  )
}

function buildMediaPlan(
  selection: MediaEpisodeSelection,
  items: OpenListObject[],
): RenameObject[] | null {
  const startIndex = selection.episodes.findIndex(
    (episode) => episode.episode_number === selection.startEpisodeNumber,
  )
  if (startIndex < 0 || selection.episodes.length - startIndex < items.length) {
    return null
  }

  return items.map((item, index) => ({
    src_name: item.name,
    new_name: buildEpisodeRename(
      selection.item,
      selection.season,
      selection.episodes[startIndex + index],
      item.name,
      false,
    ),
  }))
}

export function buildBatchRenamePlan(
  config: BatchRenameConfig,
  selectedItems: OpenListObject[],
  allItems: OpenListObject[],
): RenamePlanResult {
  if (selectedItems.length === 0) {
    return { ok: false, code: "noSelection" }
  }

  let renameObjects: RenameObject[]
  if (config.mode === "regex") {
    const plan = buildRegexPlan(config, selectedItems)
    if (!plan) {
      return { ok: false, code: "invalidRegex" }
    }
    renameObjects = plan
  } else if (config.mode === "sequence") {
    const plan = buildSequencePlan(config, selectedItems)
    if (!plan) {
      return { ok: false, code: "invalidSequence" }
    }
    renameObjects = plan
  } else if (config.mode === "find") {
    renameObjects = buildFindPlan(config, selectedItems)
  } else {
    if (!selectedItems.every(isVideoFile)) {
      return { ok: false, code: "mediaVideoOnly" }
    }
    if (!config.selection) {
      return { ok: false, code: "mediaSelectionRequired" }
    }
    if (config.selection.item.media_type !== "tv") {
      return { ok: false, code: "mediaSelectionRequired" }
    }
    const plan = buildMediaPlan(config.selection, selectedItems)
    if (!plan) {
      return { ok: false, code: "insufficientEpisodes" }
    }
    renameObjects = plan
  }

  return validatePlan(renameObjects, allItems)
}
