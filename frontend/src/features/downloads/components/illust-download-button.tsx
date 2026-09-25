import { HugeiconsIcon } from "@hugeicons/react";
import { type MouseEvent, useEffect, useRef, useState } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAuth } from "@/features/auth";
import { downloadIllustViaBrowser } from "@/features/downloads/browser-download";
import { GuestDownloadButton } from "@/features/downloads/guest-download-button";
import type { Illust } from "@/features/illusts/api";
import { isRestricted, useR18Mask } from "@/features/illusts/r18";
import { useIllustDownload } from "@/features/illusts/use-illust-download";
import { useMessages } from "@/i18n";
import { CheckIcon, DownloadIcon } from "@/lib/icons";
import { cn } from "@/lib/utils";

type IllustDownloadButtonProps = {
    // Full work (not just the id): anonymous sessions save through the
    // browser via downloadIllustViaBrowser and need the page URLs; operator
    // sessions still enqueue a server job from the id alone.
    illust: Illust;
    className?: string;
};

const RING_RADIUS = 18;
const RING_CIRCUM = 2 * Math.PI * RING_RADIUS;
// Quarter-arc visible in indeterminate mode; combined with animate-spin gives a
// classic Material-style indeterminate spinner.
const INDETERMINATE_OFFSET = RING_CIRCUM * 0.75;

function IllustDownloadButton({ illust, className }: IllustDownloadButtonProps) {
    const m = useMessages();
    const { status } = useAuth();
    const authenticated = !!status?.authenticated;
    const { downloading, justSent, errorTitle, percent, indeterminate, trigger } = useIllustDownload(illust.id);
    const [guestBusy, setGuestBusy] = useState(false);
    const [guestDone, setGuestDone] = useState(false);
    const guestTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(
        () => () => {
            if (guestTimerRef.current) clearTimeout(guestTimerRef.current);
        },
        [],
    );

    // Masked restricted works (zh-CN) are hard-blocked: no download at all.
    const r18Mask = useR18Mask();
    const masked = isRestricted(illust.x_restrict) && r18Mask;
    if (masked) return null;

    // Card download: operator sessions enqueue a server job; public-mode
    // guests get the single-file guest download (first page, through the
    // public image proxies). Local-mode guests keep the anonymous
    // browser-save path.
    if (status?.public_read && !status?.authenticated) {
        return (
            <GuestDownloadButton
                illust={illust}
                pageIndex={0}
                label={m.downloads_btn_download()}
                className="absolute right-3.5 bottom-3.5 flex size-10 scale-90 items-center justify-center rounded-xl bg-primary text-primary-foreground opacity-0 shadow-md transition-all duration-300 group-hover:scale-100 group-hover:opacity-100"
            />
        );
    }

    const guestDownload = async () => {
        if (guestBusy) return;
        setGuestBusy(true);
        setGuestDone(false);
        try {
            // Same outcome gating as the viewer's DownloadCell: the check
            // only flashes when every page actually saved.
            const result = await downloadIllustViaBrowser(illust);
            if (result === "saved") {
                setGuestDone(true);
                if (guestTimerRef.current) clearTimeout(guestTimerRef.current);
                guestTimerRef.current = setTimeout(() => setGuestDone(false), 1400);
            }
        } finally {
            setGuestBusy(false);
        }
    };

    const onClick = (e: MouseEvent<HTMLButtonElement>) => {
        e.stopPropagation();
        // Anonymous: browser download (no POST /downloads ever leaves the tab).
        if (!authenticated) {
            void guestDownload();
            return;
        }
        trigger();
    };

    const busy = downloading || guestBusy;
    const dashOffset = indeterminate ? INDETERMINATE_OFFSET : RING_CIRCUM * (1 - (percent ?? 0));

    const colorClasses = errorTitle
        ? "bg-destructive text-white ring-2 ring-destructive/40"
        : busy
          ? "bg-accent text-accent-foreground"
          : "bg-primary text-primary-foreground";

    const showCheck = justSent || guestDone;

    const button = (
        <button
            type="button"
            onClick={onClick}
            disabled={busy}
            aria-label={busy ? m.downloads_btn_downloading() : m.downloads_btn_download()}
            className={cn(
                "absolute right-3.5 bottom-3.5 flex size-10 scale-90 items-center justify-center opacity-0 shadow-md transition-all duration-300 disabled:cursor-wait group-hover:scale-100 group-hover:opacity-100",
                colorClasses,
                busy || showCheck ? "rounded-[20px]" : "rounded-xl",
                (busy || showCheck) && "scale-100 opacity-100 group-hover:opacity-100",
                !busy && "disabled:opacity-70 group-hover:disabled:opacity-70",
                className,
            )}
        >
            {downloading && (
                <span
                    className={cn(
                        "pointer-events-none absolute inset-0 flex items-center justify-center",
                        indeterminate && "animate-spin",
                    )}
                >
                    <svg
                        viewBox="0 0 40 40"
                        className={cn("size-full", !indeterminate && "-rotate-90")}
                        role="img"
                        aria-label={m.downloads_btn_progress()}
                    >
                        <title>{m.downloads_btn_progress()}</title>
                        <circle
                            cx="20"
                            cy="20"
                            r={RING_RADIUS}
                            fill="none"
                            strokeWidth="2"
                            className="stroke-accent-foreground/30"
                        />
                        <circle
                            cx="20"
                            cy="20"
                            r={RING_RADIUS}
                            fill="none"
                            strokeWidth="2"
                            strokeLinecap="round"
                            className="stroke-accent-foreground"
                            strokeDasharray={RING_CIRCUM}
                            strokeDashoffset={dashOffset}
                            style={{ transition: indeterminate ? "none" : "stroke-dashoffset 250ms linear" }}
                        />
                    </svg>
                </span>
            )}
            <HugeiconsIcon icon={showCheck ? CheckIcon : DownloadIcon} size={16} strokeWidth={1.5} />
        </button>
    );

    if (!errorTitle) return button;
    return (
        <Tooltip>
            <TooltipTrigger render={button} />
            <TooltipContent>{errorTitle}</TooltipContent>
        </Tooltip>
    );
}

export default IllustDownloadButton;
