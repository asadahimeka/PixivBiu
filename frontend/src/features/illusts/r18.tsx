import { useLocale } from "@/i18n";

// R-18 masking is locale-driven on the public site: zh-CN blurs restricted
// works behind a label, other locales show covers as-is. The viewer reveals
// on click; cards and strips stay blurred (clicking opens the viewer, where
// reveal is possible).

export function isRestricted(xRestrict: number): boolean {
    // 1 = R-18, 2 = R-18G (0 = all-ages).
    return xRestrict === 1 || xRestrict >= 2;
}

export function restrictLabel(xRestrict: number): string {
    return xRestrict >= 2 ? "R-18G" : "R-18";
}

export function useR18Mask(): boolean {
    const { locale } = useLocale();
    return locale === "zh-CN";
}

export function R18Badge({ xRestrict }: { xRestrict: number }) {
    return (
        <div aria-hidden className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
            <span className="rounded-full bg-black/60 px-3 py-1 font-mono text-white text-xs backdrop-blur-sm">
                {restrictLabel(xRestrict)}
            </span>
        </div>
    );
}
