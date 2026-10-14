import { describe, expect, it } from "vitest"

import {
  buildBatchRenamePlan,
  MediaEpisodeSelection,
} from "@/features/files/batchRename"
import { MediaEpisode, OpenListObject } from "@/shared/api/manager"

function file(name: string, overrides: Partial<OpenListObject> = {}): OpenListObject {
  return {
    name,
    size: 1024,
    is_dir: false,
    modified: "2026-02-03T04:05:06",
    created: "2025-01-02T03:04:05",
    sign: "",
    thumb: "",
    type: 0,
    hashinfo: "",
    ...overrides,
  }
}

function episode(number: number, name: string): MediaEpisode {
  return {
    id: number,
    episode_number: number,
    name,
    season_number: 1,
    vote_average: 8,
    vote_count: 10,
  }
}

function mediaSelection(episodes: MediaEpisode[], startEpisodeNumber: number): MediaEpisodeSelection {
  return {
    item: {
      id: 1,
      media_type: "tv",
      title: "The Show",
      overview: "",
      vote_average: 8,
    },
    season: {
      id: 10,
      episode_count: episodes.length,
      name: "Season 1",
      season_number: 1,
      vote_average: 8,
    },
    episodes,
    startEpisodeNumber,
  }
}

describe("buildBatchRenamePlan", () => {
  it("builds regex replacements with capture groups and date placeholders", () => {
    const item = file("Pilot.mkv")
    const result = buildBatchRenamePlan(
      {
        mode: "regex",
        pattern: "^(.*)\\.mkv$",
        replacement: "$1-{created_year}{created_month}{created_date}.mp4",
      },
      [item],
      [item],
    )

    expect(result).toEqual({
      ok: true,
      renameObjects: [
        { src_name: "Pilot.mkv", new_name: "Pilot-20250102.mp4" },
      ],
    })
  })

  it("rejects invalid regular expressions", () => {
    const item = file("Pilot.mkv")
    expect(
      buildBatchRenamePlan(
        { mode: "regex", pattern: "[", replacement: "" },
        [item],
        [item],
      ),
    ).toEqual({ ok: false, code: "invalidRegex" })
  })

  it("generates ordered sequential names and preserves file extensions", () => {
    const items = [
      file("Episode 10.mkv"),
      file("Season Folder", { is_dir: true }),
    ]
    const result = buildBatchRenamePlan(
      { mode: "sequence", template: "Episode-{number}", start: "7", padding: 2 },
      items,
      items,
    )

    expect(result).toEqual({
      ok: true,
      renameObjects: [
        { src_name: "Episode 10.mkv", new_name: "Episode-07.mkv" },
        { src_name: "Season Folder", new_name: "Episode-08" },
      ],
    })
  })

  it("allows find and replace to remove matched text", () => {
    const item = file("Show.1080p.mkv")
    const result = buildBatchRenamePlan(
      { mode: "find", search: ".1080p", replacement: "" },
      [item],
      [item],
    )

    expect(result).toEqual({
      ok: true,
      renameObjects: [{ src_name: "Show.1080p.mkv", new_name: "Show.mkv" }],
    })
  })

  it("rejects duplicate targets and existing-name conflicts", () => {
    const items = [file("One.mkv"), file("Two.mkv"), file("Existing.mkv")]

    expect(
      buildBatchRenamePlan(
        { mode: "regex", pattern: "^.*$", replacement: "Same.mkv" },
        items.slice(0, 2),
        items,
      ),
    ).toEqual({ ok: false, code: "duplicateTarget", name: "Same.mkv" })

    expect(
      buildBatchRenamePlan(
        { mode: "find", search: "One", replacement: "Existing" },
        [items[0]],
        items,
      ),
    ).toEqual({ ok: false, code: "nameConflict", name: "Existing.mkv" })
  })

  it("maps video files to consecutive episodes with SxxEyy names", () => {
    const items = [file("source-a.mkv"), file("source-b.mp4")]
    const episodes = [episode(1, "First"), episode(2, "Second"), episode(3, "Third")]
    const result = buildBatchRenamePlan(
      { mode: "media", selection: mediaSelection(episodes, 2) },
      items,
      items,
    )

    expect(result).toEqual({
      ok: true,
      renameObjects: [
        {
          src_name: "source-a.mkv",
          new_name: "The Show - S01E02 - Second.mkv",
        },
        {
          src_name: "source-b.mp4",
          new_name: "The Show - S01E03 - Third.mp4",
        },
      ],
    })
  })

  it("rejects non-video selections and insufficient remaining episodes", () => {
    const video = file("source.mkv")
    const ordinaryFile = file("subtitle.srt")
    const selection = mediaSelection([episode(1, "First")], 1)

    expect(
      buildBatchRenamePlan(
        { mode: "media", selection },
        [video, ordinaryFile],
        [video, ordinaryFile],
      ),
    ).toEqual({ ok: false, code: "mediaVideoOnly" })

    expect(
      buildBatchRenamePlan(
        { mode: "media", selection },
        [video, file("second.mp4")],
        [video, file("second.mp4")],
      ),
    ).toEqual({ ok: false, code: "insufficientEpisodes" })
  })
})
