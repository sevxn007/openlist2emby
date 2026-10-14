import {
  MediaLibrary,
  MediaLibraryItem,
  MediaLibraryItemKind,
} from "@/shared/api/manager"

export type MediaItemLayout = "poster" | "landscape" | "square" | "list"

export function mediaItemLayout(kind: MediaLibraryItemKind): MediaItemLayout {
  if (kind === "episode") return "landscape"
  if (kind === "audio") return "list"
  if (kind === "music_artist" || kind === "music_album") return "square"
  return "poster"
}

export function mediaLayoutForContext(
  library: MediaLibrary,
  parent?: MediaLibraryItem,
): MediaItemLayout {
  if (parent?.kind === "season") return "landscape"
  if (parent?.kind === "music_album") return "list"
  if (library.content_type === "music") return "list"
  return "poster"
}

export function mediaGridClassName(layout: MediaItemLayout) {
  if (layout === "landscape") {
    return "grid grid-cols-1 gap-x-4 gap-y-6 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4"
  }
  if (layout === "square") {
    return "grid grid-cols-2 gap-x-3 gap-y-5 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 2xl:grid-cols-8"
  }
  if (layout === "list") return "divide-y border-y"
  return "grid grid-cols-2 gap-x-3 gap-y-5 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 2xl:grid-cols-7"
}

export function mediaRailClassName(layout: MediaItemLayout) {
  if (layout === "landscape") {
    return "grid min-w-0 grid-flow-col auto-cols-[85%] gap-4 overflow-x-auto pb-2 scrollbar-none sm:auto-cols-[calc((100%-1rem)/2)] xl:auto-cols-[calc((100%-3rem)/4)]"
  }
  if (layout === "square") {
    return "grid min-w-0 grid-flow-col auto-cols-[calc((100%-1rem)/2)] gap-4 overflow-x-auto pb-2 scrollbar-none sm:auto-cols-[calc((100%-2rem)/3)] md:auto-cols-[calc((100%-3rem)/4)] xl:auto-cols-[calc((100%-7rem)/8)]"
  }
  if (layout === "list") {
    return "grid min-w-0 grid-flow-col auto-cols-[85%] gap-3 overflow-x-auto pb-2 scrollbar-none md:auto-cols-[calc((100%-0.75rem)/2)] xl:auto-cols-[calc((100%-1.5rem)/3)]"
  }
  return "grid min-w-0 grid-flow-col auto-cols-[calc((100%-1rem)/2)] gap-4 overflow-x-auto pb-2 scrollbar-none sm:auto-cols-[calc((100%-2rem)/3)] md:auto-cols-[calc((100%-3rem)/4)] xl:auto-cols-[calc((100%-7rem)/8)]"
}

export function isFolderItem(item: MediaLibraryItem) {
  return ["series", "season", "music_artist", "music_album"].includes(item.kind)
}

export function displayLyrics(value?: string) {
  const text = value?.trim()
  if (!text) return ""
  return text.replace(/^\[(?:\d{1,3}:\d{2}(?:[.:]\d{1,3})?|[a-z]+:[^\]]*)\]\s*/gim, "").trim()
}
