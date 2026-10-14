import { describe, expect, it, vi } from "vitest"

import {
  getProgressLabel,
  getTaskSubject,
  getTaskPollingInterval,
  isTaskActive,
  isTaskRetryable,
} from "@/features/tasks/taskModel"
import type { TaskRunStatus, TaskType } from "@/shared/api/manager"

const retainedTypes: TaskType[] = [
  "music.embed_metadata",
  "media.scan",
  "media.metadata.relocate",
].map((type) => ({
  type,
  payload_version: 1,
  manual: true,
  schedulable: type === "media.scan",
  cancelable: true,
}))

describe("task polling", () => {
  it("polls active tasks once per second and stops for terminal or empty state", () => {
    expect(getTaskPollingInterval(isTaskActive("queued"))).toBe(1_000)
    expect(getTaskPollingInterval(isTaskActive("running"))).toBe(1_000)
    expect(getTaskPollingInterval(isTaskActive("succeeded"))).toBe(false)
    expect(getTaskPollingInterval(false)).toBe(false)
  })
})

describe("task retry eligibility", () => {
  it.each(retainedTypes)("retries failed and canceled $type runs only", ({ type }) => {
    const statuses: TaskRunStatus[] = ["queued", "running", "succeeded", "failed", "canceled"]
    for (const status of statuses) {
      expect(isTaskRetryable({ type, status }, retainedTypes)).toBe(
        status === "failed" || status === "canceled",
      )
    }
  })

  it.each(["files.sync", "music.catalog_upload", "unknown.type"])(
    "does not retry unregistered %s history",
    (type) => {
      expect(isTaskRetryable({ type, status: "failed" }, retainedTypes)).toBe(false)
      expect(isTaskRetryable({ type, status: "canceled" }, retainedTypes)).toBe(false)
    },
  )

  it("does not offer retry before task types are loaded", () => {
    expect(isTaskRetryable({ type: "media.scan", status: "failed" }, [])).toBe(false)
  })
})

describe("media scan progress", () => {
  it("includes live directory and file counts", () => {
    const translate = vi.fn(() => "scan counts")
    expect(
      getProgressLabel(
        {
          type: "media.scan",
          status: "running",
          progress: {
            stage: "scanning",
            percent: 0,
            directories_scanned: 12,
            files_scanned: 34,
          },
        },
        translate,
      ),
    ).toBe("scan counts")
    expect(translate).toHaveBeenCalledWith("mediaLibrary.scanning", {
      directories: 12,
      files: 34,
      percent: 0,
    })
  })

  it("labels the screenshot stage", () => {
    expect(
      getProgressLabel(
        {
          type: "media.scan",
          status: "running",
          progress: { stage: "screenshots", percent: 50 },
        },
        (key, values) => `${key}:${values?.percent ?? ""}`,
      ),
    ).toBe("mediaLibrary.capturingScreenshots:50")
  })

  it("labels music metadata localization without treating 99% as completed", () => {
    expect(
      getProgressLabel(
        {
          type: "media.scan",
          status: "running",
          progress: { stage: "localizing", percent: 99 },
        },
        (key, values) => `${key}:${values?.percent ?? ""}`,
      ),
    ).toBe("mediaLibrary.localizing:99")
  })
})

describe("task subjects", () => {
	it("uses the media scan label for historical runs without a library name", () => {
		expect(
		getTaskSubject(
			{ id: 7, type: "media.scan", subject: { library_id: 42 } },
        () => "媒体库扫描",
      ),
		).toBe("媒体库扫描")
	})

	it("uses the scanned library name for media scan runs", () => {
		expect(
		getTaskSubject(
			{
				id: 7,
				type: "media.scan",
				subject: { library_id: 42, library_name: "电影" },
			},
			(key, values) =>
				key === "tasks.subject.mediaScan"
					? `${values?.library}媒体库扫描`
					: "媒体库扫描",
		),
		).toBe("电影媒体库扫描")
  })
})
