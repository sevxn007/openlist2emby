import type { TaskRunFilter, TaskRunItemFilter } from "@/shared/api/manager"

export const activeTaskFilter = {
  statuses: ["queued", "running"],
  page: 1,
  perPage: 1,
} as const satisfies TaskRunFilter

export const taskQueryKeys = {
  all: ["tasks"] as const,
	openList: () => ["tasks", "openlist"] as const,
  types: () => ["tasks", "types"] as const,
  schedules: (type = "") => ["tasks", "schedules", { type }] as const,
  schedule: (id: number) => ["tasks", "schedules", id] as const,
  runs: (filter: TaskRunFilter = {}) =>
    [
      "tasks",
      "runs",
      {
        page: filter.page ?? 1,
        perPage: filter.perPage ?? 20,
        status: filter.statuses?.join(",") ?? "",
        type: filter.type ?? "",
      },
    ] as const,
  run: (id: number) => ["tasks", "runs", id] as const,
  runItems: (id: number, filter: TaskRunItemFilter = {}) =>
    [
      "tasks",
      "runs",
      id,
      "items",
      {
        status: filter.status ?? "",
        action: filter.action ?? "",
        page: filter.page ?? 1,
        perPage: filter.perPage ?? 20,
      },
    ] as const,
}
