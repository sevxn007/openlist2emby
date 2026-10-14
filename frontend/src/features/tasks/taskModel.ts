import type { TaskRun, TaskRunStatus, TaskSchedule, TaskType } from "@/shared/api/manager"
import type { TranslationKey } from "@/shared/i18n/messages"

export const taskRunStatuses: TaskRunStatus[] = [
  "queued",
  "running",
  "succeeded",
  "failed",
  "canceled",
]

export function isTaskActive(status: TaskRunStatus | undefined) {
  return status === "queued" || status === "running"
}

export function isTaskTerminal(status: TaskRunStatus | undefined) {
  return status === "succeeded" || status === "failed" || status === "canceled"
}

export function isTaskRetryable(
  run: Pick<TaskRun, "type" | "status">,
  types: TaskType[],
) {
  return (
    (run.status === "failed" || run.status === "canceled") &&
    types.some((descriptor) => descriptor.type === run.type)
  )
}

export function getTaskPollingInterval(hasActiveTask: boolean) {
  return hasActiveTask ? 1_000 : false
}

export function getTaskSubject(
  run: Pick<TaskRun, "id" | "subject" | "type">,
  t: (
    key: TranslationKey,
    values?: Record<string, string | number>,
  ) => string,
) {
  if (run.type === "media.scan") {
		if (
			typeof run.subject === "object" &&
			run.subject !== null &&
			"library_name" in run.subject &&
			typeof run.subject.library_name === "string" &&
			run.subject.library_name.trim() !== ""
		) {
			return t("tasks.subject.mediaScan", {
				library: run.subject.library_name,
			})
		}
    return t("tasks.type.mediaScan")
  }
  if (run.type === "media.metadata.relocate") {
    return t("tasks.type.mediaMetadataMigration")
  }
  if (
    typeof run.subject === "object" &&
    run.subject !== null &&
    "item_id" in run.subject &&
    typeof run.subject.item_id === "number"
  ) {
    return `#${run.subject.item_id}`
  }
  if (
    typeof run.subject === "object" &&
    run.subject !== null &&
    "path" in run.subject &&
    typeof run.subject.path === "string"
  ) {
    return run.subject.path
  }
  if (
    typeof run.subject === "object" &&
    run.subject !== null &&
    "library_id" in run.subject &&
    typeof run.subject.library_id === "number"
  ) {
    return `#${run.subject.library_id}`
  }
  return `#${run.id}`
}

export function scheduleToPayload(
  schedule: TaskSchedule,
  enabled = schedule.enabled,
) {
  return {
    name: schedule.name,
    type: schedule.type,
    cron_expression: schedule.cron_expression,
    timezone: schedule.timezone,
    enabled,
    payload: schedule.parameters ?? {},
  }
}

export function getTaskTypeLabel(
  type: string,
  t: (key: TranslationKey) => string,
) {
  if (type === "music.embed_metadata") {
    return t("tasks.type.musicEmbed")
  }
  if (type === "media.scan") {
    return t("tasks.type.mediaScan")
  }
  if (type === "media.metadata.relocate") {
    return t("tasks.type.mediaMetadataMigration")
  }
  return type
}

export function getProgressLabel(
  run: Pick<TaskRun, "progress" | "status" | "type">,
  t: (
    key: TranslationKey,
    values?: Record<string, string | number>,
  ) => string,
) {
  const stageKeys: Record<string, TranslationKey> = {
    scanning: "mediaLibrary.scanning",
    preparing: "music.progress.preparing",
    fetching_metadata: "music.progress.fetchingMetadata",
    downloading: "music.progress.downloading",
    embedding: "music.progress.embedding",
    uploading: "music.progress.uploading",
    completed: "music.progress.completed",
    indexing: "mediaLibrary.indexing",
    localizing: "mediaLibrary.localizing",
    metadata: "mediaLibrary.fetchingMusicMetadata",
    screenshots: "mediaLibrary.capturingScreenshots",
    copying: "settings.mediaMetadataMigrating",
  }
  const key = stageKeys[run.progress.stage]
  return key
    ? t(key, {
        directories: run.progress.directories_scanned ?? 0,
        files: run.progress.files_scanned ?? 0,
        percent: run.progress.percent,
      })
    : t(`tasks.status.${run.status}`)
}
