// Browser-side download for anonymous/public sessions: the server never
// streams artwork bytes for guests and no /downloads job is enqueued.
// Candidates come from rewritePximgCandidates (public proxies first, the
// same-origin image proxy last). Each candidate is tried with
// fetch → blob → objectURL → a[download]; CORS/network/type failures move to
// the next candidate, and when every candidate fails the first one is opened
// in a new tab as a last resort.
import { type Illust, illustZoomUrl } from "@/features/illusts/api";
import { rewritePximgCandidates } from "@/lib/pixiv-image";

// Pixiv names originals `<id>_p<N>.<ext>` — reuse that as the save filename so
// multi-page works stay distinguishable; fall back to a generic name if the
// URL can't be parsed.
export function filenameFromUrl(imageUrl: string): string {
    try {
        const base = decodeURIComponent(new URL(imageUrl).pathname.split("/").pop() ?? "");
        if (base) return base;
    } catch {
        // unparseable URL — use the fallback below
    }
    return "image.jpg";
}

export type BrowserDownloadResult = "saved" | "opened" | "failed";

export async function downloadViaBrowser(imageUrl: string, filename: string): Promise<BrowserDownloadResult> {
    const candidates = rewritePximgCandidates(imageUrl);
    for (const c of candidates) {
        try {
            const res = await fetch(c, { mode: "cors" });
            if (!res.ok) continue;
            // A proxy soft-failure answers with an error page, not the image —
            // saving that blob under the artwork filename would be wrong.
            const contentType = res.headers.get("content-type") ?? "";
            if (contentType.includes("text/html") || contentType.includes("application/json")) continue;
            const blob = await res.blob();
            const obj = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = obj;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(obj), 30_000);
            return "saved";
        } catch {
            // CORS/network failure on this candidate — fall through to the next.
        }
    }
    // No candidate at all is a real failure: "opened" must keep meaning
    // "we actually opened a fallback tab" so callers never flash the
    // success affordance on a no-op.
    const fallback = candidates[0];
    if (!fallback) return "failed";
    window.open(fallback, "_blank", "noopener");
    return "opened";
}

// Saves every page of `illust` through downloadViaBrowser and reports the
// overall outcome: "saved" only when ALL pages saved (that is the only result
// callers may flash a check for), "opened" when a fallback tab interrupted the
// run (stop — don't stack one tab per page), "failed" when there was nothing
// to download. Same multi-page predicate as illustZoomUrl: page_count alone
// can exceed an empty meta_pages (list payloads), which would replay p0.
export async function downloadIllustViaBrowser(illust: Illust): Promise<BrowserDownloadResult> {
    const pageCount = illust.page_count > 1 && illust.meta_pages.length > 0 ? illust.page_count : 1;
    for (let i = 0; i < pageCount; i++) {
        const imageUrl = illustZoomUrl(illust, i);
        const result = await downloadViaBrowser(imageUrl, filenameFromUrl(imageUrl));
        if (result !== "saved") return result;
    }
    return "saved";
}
