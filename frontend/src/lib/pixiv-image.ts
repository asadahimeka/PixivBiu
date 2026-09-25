// Rewrites i.pximg.net image URLs into an ordered try-list of public
// proxies (third-party mirrors that fetch with the Pixiv Referer), keeping
// the same-origin backend proxy (GET /api/v1/proxy/img, which disk-caches)
// as the last fallback. rewritePximgUrl returns the first candidate.
const PXIMG_HOST = "i.pximg.net";
const PUBLIC_PROXIES: string[] = [
    "https://i.pixiv.re",
    "https://img.rika.club",
    "https://web.pximg.cc",
    "https://i.muxmus.com",
    "https://prox.spacetimee.xyz",
];
const SAME_ORIGIN_FALLBACK = "/api/v1/proxy/img";

export function rewritePximgCandidates(url: string | null | undefined): string[] {
    if (!url) return [];
    // Match on the actual host, not a substring: a plain includes() would re-wrap
    // an already-proxied URL (/api/v1/proxy/img?url=…i.pximg.net…) or anything that
    // merely contains the hostname as text. Non-absolute URLs (e.g. an
    // already-proxied relative path) fail to parse and are left untouched.
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return [url];
    }
    if (u.hostname !== PXIMG_HOST) return [url];
    const path = `${u.pathname}${u.search}`;
    const out = PUBLIC_PROXIES.map((p) => `${p}${path}`);
    out.push(`${SAME_ORIGIN_FALLBACK}?url=${encodeURIComponent(url)}`);
    return out;
}

export function rewritePximgUrl(url: string | null | undefined): string {
    return rewritePximgCandidates(url)[0] ?? "";
}
