import { type ReactNode, useEffect, useRef, useState } from "react";
import { rewritePximgCandidates } from "@/lib/pixiv-image";
import { cn } from "@/lib/utils";

type PximgImageProps = {
    src: string | null | undefined;
    alt: string;
    fallback: ReactNode;
    className?: string;
    // How the <img> fills its box. Defaults to "cover" (thumbnails/cards). The
    // viewer stage passes "contain" so the whole artwork is visible — note
    // `className` styles the wrapper, not the <img>, so fit can't be a className.
    fit?: "cover" | "contain";
    onLoad?: (img: HTMLImageElement) => void;
};

function PximgImage({ src, alt, fallback, className, fit = "cover", onLoad }: PximgImageProps) {
    // Ordered try-list: public proxies first, same-origin backend proxy last.
    const candidates = rewritePximgCandidates(src);
    const imgRef = useRef<HTMLImageElement>(null);
    const [loaded, setLoaded] = useState(false);
    const [errored, setErrored] = useState(false);
    // Which candidate the <img> is currently showing; onError advances it.
    const [srcIndex, setSrcIndex] = useState(0);
    const index = Math.min(srcIndex, Math.max(candidates.length - 1, 0));
    const url = candidates[index] ?? "";

    // Reveal only after the full frame is decoded, so images don't paint
    // top-to-bottom. decode() is driven by the load event / cache-hit check
    // below, never eagerly on mount, so off-screen loading="lazy" images aren't
    // force-loaded.
    const handleLoaded = (img: HTMLImageElement) => {
        img.decode().then(
            () => setLoaded(true),
            () => setLoaded(true), // decode() can reject (e.g. src swapped); reveal anyway
        );
        onLoad?.(img);
    };

    // A new source starts back at the first candidate.
    // biome-ignore lint/correctness/useExhaustiveDependencies: re-run only on src change
    useEffect(() => {
        setSrcIndex(0);
    }, [src]);

    // A cached image can finish loading before React attaches onLoad, so the
    // event never fires and the <img> would stay opacity-0 forever. Reveal the
    // already-complete case here (this also notifies onLoad — the popover
    // preview reads naturalWidth/naturalHeight from the element to size its box).
    // Also runs when onError advances to the next candidate (url changes).
    // biome-ignore lint/correctness/useExhaustiveDependencies: re-run only on url change; handleLoaded/onLoad are recreated each render
    useEffect(() => {
        setLoaded(false);
        setErrored(false);
        const img = imgRef.current;
        if (img?.complete && img.naturalWidth > 0) handleLoaded(img);
    }, [url]);

    if (!url) return <>{fallback}</>;

    // On failure, fall through to the next candidate; only give up (show the
    // fallback over the <img>) after the last one errors.
    const handleError = () => {
        if (index + 1 < candidates.length) setSrcIndex(index + 1);
        else setErrored(true);
    };

    return (
        <div className={cn("relative overflow-hidden", className)}>
            {/* In-flow fallback: underlays the image and (with className) holds the box open. */}
            {fallback}
            {!errored && (
                <img
                    ref={imgRef}
                    src={url}
                    alt={alt}
                    loading="lazy"
                    decoding="async"
                    referrerPolicy="no-referrer"
                    onLoad={(e) => handleLoaded(e.currentTarget)}
                    onError={handleError}
                    className={cn(
                        "absolute inset-0 size-full transition-opacity duration-300",
                        fit === "contain" ? "object-contain" : "object-cover",
                        loaded ? "opacity-100" : "opacity-0",
                    )}
                />
            )}
        </div>
    );
}

export default PximgImage;
