import { useEffect, useState } from "react";

// Matches Tailwind's `md:` breakpoint: below 768px the shell switches from
// the resizable desktop sidebar to the mobile top bar + drawer.
const QUERY = "(max-width: 767px)";

export function useIsMobile(): boolean {
    const [isMobile, setIsMobile] = useState(() => typeof window !== "undefined" && window.matchMedia(QUERY).matches);

    useEffect(() => {
        const mql = window.matchMedia(QUERY);
        const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches);
        setIsMobile(mql.matches);
        mql.addEventListener("change", onChange);
        return () => mql.removeEventListener("change", onChange);
    }, []);

    return isMobile;
}
