import {
  MEDIA_SCAN_TASK_TYPE,
  type MediaScanScheduleParameters,
  type TaskSchedule,
} from "@/shared/api/manager"

export function getMediaScanScheduleParameters(
  schedule: TaskSchedule,
): MediaScanScheduleParameters | null {
  if (
    schedule.type !== MEDIA_SCAN_TASK_TYPE ||
    typeof schedule.parameters !== "object" ||
    schedule.parameters === null
  ) {
    return null
  }
  const parameters = schedule.parameters as Record<string, unknown>
  if (
    typeof parameters.library_id !== "number" ||
    !Number.isInteger(parameters.library_id) ||
    parameters.library_id <= 0
  ) {
    return null
  }
  return {
    library_id: parameters.library_id,
    library_name:
      typeof parameters.library_name === "string"
        ? parameters.library_name
        : undefined,
  }
}

export function mediaScanSchedulesByLibrary(schedules: TaskSchedule[]) {
  const byLibrary = new Map<number, TaskSchedule>()
  for (const schedule of schedules) {
    const parameters = getMediaScanScheduleParameters(schedule)
    if (parameters) byLibrary.set(parameters.library_id, schedule)
  }
  return byLibrary
}
