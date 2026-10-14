import {useState} from "react"
import {useQuery} from "@tanstack/react-query"
import {Search} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog"
import {Field, FieldLabel} from "@/components/ui/field"
import {Input} from "@/components/ui/input"
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from "@/components/ui/select"
import {Spinner} from "@/components/ui/spinner"
import {
    getMediaImages,
    getMediaImageSources,
    getMediaLibraryItem,
    type MediaImageQuery,
    type MediaImageSelection,
    type MediaLibraryItem,
    type MetadataSource,
    searchMedia
} from "@/shared/api/manager"
import {useI18n} from "@/shared/i18n/useI18n"

interface Props {
    item: MediaLibraryItem
    imageType: "primary" | "backdrop"
    onClose: () => void
    onSelect: (selection: MediaImageSelection) => void
}

export function MediaImagePicker({item, imageType, onClose, onSelect}: Props) {
    const {t, locale} = useI18n()
    const sources = useQuery({queryKey: ["media-image-sources"], queryFn: getMediaImageSources})
    const isChild = item.kind === "season" || item.kind === "episode"
    const parent = useQuery({
        queryKey: ["media-image-series", item.id, item.series_id, item.parent_id],
        queryFn: async () => {
            const id = item.series_id ?? item.parent_id
            if (!id) return null
            const ancestor = await getMediaLibraryItem(id)
            if (ancestor.kind === "series") return ancestor
            if (ancestor.kind === "season" && ancestor.parent_id) return getMediaLibraryItem(ancestor.parent_id)
            return null
        },
        enabled: isChild,
    })
    const [chosenSource, setChosenSource] = useState<MetadataSource>()
    const configured = sources.data?.sources ?? []
    const actual = item.resolved_metadata_source
    const source = chosenSource ?? (actual && configured.includes(actual) ? actual : configured[0])
    const [queryText, setQueryText] = useState(item.series_name ?? item.name)
    const [searchText, setSearchText] = useState("")
    const [target, setTarget] = useState<{ source: MetadataSource; id: number; name: string }>()
    const [selected, setSelected] = useState<MediaImageSelection>()
    const mediaType = item.kind === "movie" ? "movie" : "tv"
    const currentWork = isChild ? parent.data : item
    const currentID = source === "tmdb" ? currentWork?.tmdb_id : currentWork?.tvdb_id
    const targetID = target && target.source === source ? target.id : currentID
    const imageQuery: MediaImageQuery | undefined = source && targetID ? {
        source, type: mediaType, id: targetID, image_type: imageType,
        ...(isChild ? {season: item.season_number} : {}),
        ...(item.kind === "episode" ? {episode: item.episode_number} : {}),
    } : undefined
    const numbersValid = !isChild || (item.season_number !== undefined && (item.kind !== "episode" || item.episode_number !== undefined))
    const images = useQuery({
        queryKey: ["media-images", imageQuery],
        queryFn: () => getMediaImages(imageQuery!),
        enabled: !!imageQuery && numbersValid
    })
    const search = useQuery({
        queryKey: ["media-image-search", source, searchText, locale],
        queryFn: () => searchMedia(searchText, locale, source),
        enabled: !!source && !!searchText
    })
    const error = sources.error ?? (isChild ? parent.error : null) ?? images.error ?? search.error
    const loading = sources.isPending || (isChild && parent.isPending) || (!!imageQuery && images.isFetching) || (!!searchText && search.isFetching)
    const validSelection = selected && selected.query.source === source && selected.query.id === targetID ? selected : undefined

    function submitSearch() {
        setSearchText(queryText.trim());
        setSelected(undefined)
    }

    return (
        <Dialog open onOpenChange={(open) => {
            if (!open) onClose()
        }}>
            <DialogContent className="flex max-h-[calc(100dvh-2rem)] min-w-0 flex-col overflow-hidden sm:max-w-3xl"
                           aria-describedby={undefined}>
                <DialogHeader className="pr-10"><DialogTitle>{t("mediaImages.title")}</DialogTitle></DialogHeader>
                <DialogBody className="min-h-0 min-w-0 space-y-4 overflow-y-auto">
                    <div className="grid min-w-0 gap-3 sm:grid-cols-[10rem_minmax(0,1fr)]">
                        <Field><FieldLabel htmlFor="image-source">{t("mediaLibrary.metadataSource")}</FieldLabel>
                            <Select value={source ?? ""} onValueChange={(value: MetadataSource) => {
                                setChosenSource(value);
                                setTarget(undefined);
                                setSelected(undefined);
                                setSearchText("")
                            }} disabled={!configured.length}>
                                <SelectTrigger id="image-source" className="w-full"><SelectValue/></SelectTrigger>
                                <SelectContent>{configured.map((value) => <SelectItem key={value}
                                                                                      value={value}>{value.toUpperCase()}</SelectItem>)}</SelectContent>
                            </Select>
                        </Field>
                        <Field><FieldLabel htmlFor="image-search">{t("mediaImages.searchLabel")}</FieldLabel>
                            <div className="flex min-w-0 gap-2">
                                <Input id="image-search" value={queryText}
                                       onChange={(event) => setQueryText(event.target.value)} onKeyDown={(event) => {
                                    if (event.key === "Enter") {
                                        event.preventDefault();
                                        submitSearch()
                                    }
                                }}/>
                                <Button type="button" variant="outline" disabled={!source || !queryText.trim()}
                                        onClick={submitSearch}><Search aria-hidden="true"/>{t("mediaImages.search")}
                                </Button>
                            </div>
                        </Field>
                    </div>
                    {!sources.isPending && !configured.length && !sources.error ?
                        <p role="status">{t("mediaImages.noSources")}</p> : null}
                    {!numbersValid ? <p role="status">{t("mediaImages.missingNumbers")}</p> : null}
                    {loading ? <div role="status" aria-label={t("mediaImages.loading")}><Spinner/></div> : null}
                    {error ?
                        <div role="alert" className="space-y-2"><p>{t("mediaImages.failed")}</p><Button type="button"
                                                                                                        variant="outline"
                                                                                                        onClick={() => {
                                                                                                            if (sources.error) void sources.refetch();
                                                                                                            if (parent.error) void parent.refetch();
                                                                                                            if (images.error) void images.refetch();
                                                                                                            if (search.error) void search.refetch()
                                                                                                        }}>{t("mediaImages.retry")}</Button>
                        </div> : null}
                    {searchText && search.data ? <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                        {search.data.results.filter((result) => result.media_type === mediaType).map((result) => <Button
                            key={result.id} type="button" variant="outline"
                            className="h-auto min-h-11 justify-start whitespace-normal text-left" onClick={() => {
                            if (!source) return;
                            setTarget({source, id: result.id, name: result.title || result.name || ""});
                            setSearchText("");
                            setSelected(undefined)
                        }}>{result.title || result.name} {(result.release_date || result.first_air_date || "").slice(0, 4)}</Button>)}
                        {!search.data.results.some((result) => result.media_type === mediaType) ?
                            <p role="status">{t("mediaImages.noResults")}</p> : null}
                    </div> : null}
                    {target ? <p className="break-words text-sm">{target.name}</p> : null}
                    {!loading && !error && source && !targetID ?
                        <p role="status">{t("mediaImages.chooseWork")}</p> : null}
                    {images.data && !searchText ?
                        <div className="grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                            {images.data.images.map((image, index) => <button
                                key={`${image.url}:${image.language ?? ""}`} type="button"
                                aria-label={t("mediaImages.candidate", {number: index + 1})}
                                aria-pressed={validSelection?.image.url === image.url && validSelection.image.language === image.language}
                                className="min-h-11 min-w-0 overflow-hidden rounded-md border-2 border-transparent bg-muted p-1 text-left focus-visible:outline-2 focus-visible:outline-ring aria-pressed:border-primary"
                                onClick={() => {
                                    if (imageQuery) setSelected({query: imageQuery, image})
                                }}>
                                <img src={image.thumbnail_url} alt="" loading="lazy"
                                     className={`${imageType === "primary" && item.kind !== "episode" ? "aspect-[2/3]" : "aspect-video"} w-full object-contain`}/>
                                <span
                                    className="block break-words p-2 text-xs">{image.width && image.height ? `${image.width} × ${image.height}` : ""}{image.language ? ` · ${image.language}` : ""}</span>
                            </button>)}
                            {!images.data.images.length ?
                                <p role="status" className="col-span-full">{t("mediaImages.empty")}</p> : null}
                        </div> : null}
                </DialogBody>
                <DialogFooter><Button type="button" variant="outline"
                                      onClick={onClose}>{t("settings.cancel")}</Button><Button type="button"
                                                                                               disabled={!validSelection || loading}
                                                                                               onClick={() => {
                                                                                                   if (validSelection) onSelect(validSelection)
                                                                                               }}>{t("mediaImages.use")}</Button></DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
