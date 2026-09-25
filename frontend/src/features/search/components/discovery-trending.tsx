import { HugeiconsIcon } from "@hugeicons/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import PximgImage from "@/components/pximg-image";
import { Skeleton } from "@/components/ui/skeleton";
import { isRestricted, R18Badge, useR18Mask } from "@/features/illusts/r18";
import { type TrendingTag, trendingTagsQueryOptions } from "@/features/search/api";
import { useMessages } from "@/i18n";
import { TagIcon } from "@/lib/icons";
import { cn } from "@/lib/utils";

const TRENDING_LIMIT = 18;

function tagLabel(t: TrendingTag): string {
    return t.translated_name ?? t.tag;
}

// Trending tags with one sample artwork each — straight from the
// /search/trending-tags feed (Pixiv's own trending-tags-illust, authenticated
// by the anonymous read pool), not the derived-from-ranking approximation.
function DiscoveryTrending() {
    const m = useMessages();
    const navigate = useNavigate();
    const r18Mask = useR18Mask();
    const query = useQuery(trendingTagsQueryOptions());
    const tags = query.data?.trend_tags.slice(0, TRENDING_LIMIT) ?? [];

    return (
        <section className="flex flex-col gap-3">
            <div className="flex items-baseline gap-2">
                <h2 className="m-0 font-medium text-2xl text-foreground leading-tight">{m.search_trending_title()}</h2>
                <span className="font-mono text-muted-foreground text-xs">{m.search_trending_daily()}</span>
            </div>
            {query.isPending && (
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-6">
                    {Array.from({ length: 12 }).map((_, i) => (
                        // biome-ignore lint/suspicious/noArrayIndexKey: skeleton placeholders
                        <div key={i} className="flex flex-col gap-2">
                            <Skeleton className="aspect-square w-full rounded-2xl" />
                            <Skeleton className="h-3.5 w-20" />
                        </div>
                    ))}
                </div>
            )}
            {query.isError && <div className="text-muted-foreground text-sm">{m.search_trending_error()}</div>}
            {query.isSuccess &&
                (tags.length === 0 ? (
                    <div className="text-lg text-muted-foreground">{m.common_empty()}</div>
                ) : (
                    <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-6">
                        {tags.map((t) => (
                            <button
                                key={t.tag}
                                type="button"
                                onClick={() => navigate(`/search/${encodeURIComponent(t.tag)}`)}
                                title={tagLabel(t) === t.tag ? undefined : t.tag}
                                className="group flex flex-col gap-1.5 text-left"
                            >
                                <span className="relative block aspect-square w-full overflow-hidden rounded-2xl bg-muted">
                                    <PximgImage
                                        src={t.illust.image_urls.square_medium ?? t.illust.image_urls.medium}
                                        alt={tagLabel(t)}
                                        fallback={
                                            <span className="flex size-full items-center justify-center text-muted-foreground">
                                                <HugeiconsIcon icon={TagIcon} size={20} strokeWidth={1.5} />
                                            </span>
                                        }
                                        className={cn(
                                            "size-full transition-transform duration-300 group-hover:scale-105",
                                            isRestricted(t.illust.x_restrict) && r18Mask && "scale-110 blur-xl",
                                        )}
                                    />
                                    {isRestricted(t.illust.x_restrict) && r18Mask && (
                                        <R18Badge xRestrict={t.illust.x_restrict} />
                                    )}
                                </span>
                                <span className="truncate text-muted-foreground text-xs group-hover:text-foreground">
                                    {tagLabel(t)}
                                </span>
                            </button>
                        ))}
                    </div>
                ))}
        </section>
    );
}

export default DiscoveryTrending;
