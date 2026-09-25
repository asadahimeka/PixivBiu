import { HugeiconsIcon } from "@hugeicons/react";
import type { MouseEvent } from "react";
import { type Illust, illustZoomUrl } from "@/features/illusts/api";
import { useMessages } from "@/i18n";
import { DownloadIcon } from "@/lib/icons";
import { rewritePximgCandidates } from "@/lib/pixiv-image";
import { cn } from "@/lib/utils";
import { filenameFromUrl } from "./browser-download";

// Guest single-file download: a literal <a download> pointed at the FIRST
// public image-proxy candidate, so the artwork bytes never flow through this
// deployment's server. The attribute is only honored same-origin, so the
// cross-origin proxy opens in a new tab instead — the accepted fallback; the
// browser's own save handles it from there.

// Animated ugoira preview AND download use the public AVIF conversion
// service's file directly (same cross-origin new-tab behavior). Third-party
// availability is not guaranteed; the stage falls back to the static frame
// through the img's onError path.
export function ugoiraAvifUrl(illustId: number): string {
    return `https://ugoira.perennialte.ch/ugoira/${illustId}`;
}

type GuestDownloadButtonProps = {
    illust: Illust;
    pageIndex: number;
    className?: string;
    label?: string;
};

export function GuestDownloadButton({ illust, pageIndex, className, label }: GuestDownloadButtonProps) {
    const m = useMessages();
    const text = label ?? m.downloads_btn_download();
    const stop = (e: MouseEvent) => e.stopPropagation();

    const isUgoira = illust.type === "ugoira";
    const url = isUgoira ? ugoiraAvifUrl(illust.id) : illustZoomUrl(illust, pageIndex);
    const href = isUgoira ? url : rewritePximgCandidates(url)[0];
    const filename = isUgoira ? `${illust.id}_ugoira.avif` : filenameFromUrl(url);

    return (
        <a
            href={href}
            download={filename}
            target="_blank"
            rel="noopener noreferrer"
            onClick={stop}
            aria-label={text}
            className={cn(className)}
        >
            <HugeiconsIcon icon={DownloadIcon} size={18} strokeWidth={1.8} />
        </a>
    );
}
