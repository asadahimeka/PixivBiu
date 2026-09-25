import { HugeiconsIcon } from "@hugeicons/react";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale, useMessages } from "@/i18n";
import { ChevronDownIcon, ExternalLinkIcon, GlobeIcon } from "@/lib/icons";

// Public-site sidebar footer: language switch + project link. No identity, no
// version (the system version endpoint is closed in public mode) — just the
// two things an anonymous visitor can legitimately want.

const LOCALES: { value: string; label: string }[] = [
    { value: "zh-CN", label: "简体中文" },
    { value: "zh-TW", label: "繁體中文" },
    { value: "en", label: "English" },
    { value: "ja", label: "日本語" },
];

function LanguageMenu() {
    const m = useMessages();
    const { locale, applyLanguage } = useLocale();
    const current = LOCALES.find((l) => l.value === locale) ?? LOCALES[2];
    return (
        <DropdownMenu>
            <DropdownMenuTrigger
                render={
                    <button
                        type="button"
                        aria-label={m.nav_language()}
                        className="flex w-full items-center gap-3 rounded-xl px-2 py-1.5 text-left text-muted-foreground text-sm transition-colors hover:bg-sidebar-accent hover:text-foreground"
                    >
                        <HugeiconsIcon icon={GlobeIcon} size={16} strokeWidth={1.5} />
                        <span className="min-w-0 flex-1 truncate">{current.label}</span>
                        <HugeiconsIcon icon={ChevronDownIcon} size={14} strokeWidth={1.5} />
                    </button>
                }
            />
            <DropdownMenuContent align="start" side="top" sideOffset={8}>
                {LOCALES.map((l) => (
                    <DropdownMenuItem
                        key={l.value}
                        onClick={() => {
                            applyLanguage(l.value);
                            // Paraglide's compiled messages are module-scope:
                            // a reload is the only way the whole tree re-renders
                            // in the new locale immediately.
                            window.location.reload();
                        }}
                        className={l.value === locale ? "text-primary" : undefined}
                    >
                        {l.label}
                    </DropdownMenuItem>
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

export function SidebarFooter() {
    const m = useMessages();
    return (
        <div className="flex flex-col gap-0.5">
            <LanguageMenu />
            <a
                href="https://github.com/txperl/PixivBiu"
                target="_blank"
                rel="noopener noreferrer"
                aria-label={m.nav_about()}
                className="flex items-center gap-3 rounded-xl px-2 py-1.5 text-left text-muted-foreground text-sm transition-colors hover:bg-sidebar-accent hover:text-foreground"
            >
                <HugeiconsIcon icon={ExternalLinkIcon} size={16} strokeWidth={1.5} />
                <span className="min-w-0 flex-1 truncate">{m.nav_about()}</span>
            </a>
        </div>
    );
}
