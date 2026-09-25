import { HugeiconsIcon } from "@hugeicons/react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router";
import RootSidebar from "@/app/layouts/root-sidebar";
import LeapyLoading from "@/components/series-leapy/leapy-loading";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ActivityBar, ActivityPanel, useActivityBar } from "@/features/activity-bar";
import { useAuth } from "@/features/auth";
import { IllustViewerProvider } from "@/features/illusts/illust-viewer";
import { useMessages } from "@/i18n";
import { MenuIcon } from "@/lib/icons";
import { useIsMobile } from "@/lib/use-is-mobile";

const ACTIVITY_PANEL_DEFAULT_SIZE = 20;

function ActivityPanelSlot() {
    const { isOpen, activeItemId } = useActivityBar();
    const preferredSizeRef = useRef(ACTIVITY_PANEL_DEFAULT_SIZE);

    if (!isOpen || !activeItemId) return null;

    return (
        <>
            <ResizableHandle />
            <ResizablePanel
                id="activity-panel"
                className="window-activity-panel"
                defaultSize={`${preferredSizeRef.current}%`}
                minSize="20%"
                maxSize="40%"
                onResize={({ asPercentage }) => {
                    preferredSizeRef.current = asPercentage;
                }}
            >
                <ActivityPanel />
            </ResizablePanel>
        </>
    );
}

// Mobile shell: a slim top bar with a drawer for the sidebar. The drawer
// closes on any navigation (path/search-param change covers route links and
// the viewer's ?illust= deep link).
function MobileShell({ children }: { children: ReactNode }) {
    const m = useMessages();
    const [menuOpen, setMenuOpen] = useState(false);
    const location = useLocation();
    // Close the drawer on navigation; the ref keeps the effect's dependency
    // referenced inside the body while skipping the initial mount.
    const navKey = `${location.pathname}?${location.search}`;
    const prevNavKeyRef = useRef<string | null>(null);

    useEffect(() => {
        if (prevNavKeyRef.current !== null && prevNavKeyRef.current !== navKey) {
            setMenuOpen(false);
        }
        prevNavKeyRef.current = navKey;
    }, [navKey]);

    return (
        <div className="flex h-full flex-col overflow-hidden">
            <header className="flex h-12 shrink-0 items-center gap-2 border-border border-b px-3">
                <button
                    type="button"
                    aria-label={m.nav_open_menu()}
                    onClick={() => setMenuOpen(true)}
                    className="flex size-9 items-center justify-center rounded-xl text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground"
                >
                    <HugeiconsIcon icon={MenuIcon} size={20} strokeWidth={1.5} />
                </button>
                <span className="font-medium text-foreground text-lg">PixivBiu</span>
            </header>
            <main data-app-scroller="" className="min-h-0 flex-1 overflow-y-auto overscroll-contain bg-background">
                {children}
            </main>
            {menuOpen && (
                <div className="fixed inset-0 z-50">
                    <div
                        aria-hidden
                        onClick={() => setMenuOpen(false)}
                        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
                    />
                    <div className="absolute inset-y-0 left-0 w-[280px] max-w-[85vw] shadow-2xl">
                        <RootSidebar />
                    </div>
                </div>
            )}
        </div>
    );
}

function RootLayout() {
    const { status } = useAuth();
    const isMobile = useIsMobile();

    // First refresh is still in flight. Show a near-empty splash so the layout
    // doesn't flash a half-loaded app before we know where the user belongs —
    // children mount only AFTER auth resolves, so every child can read a
    // definite authenticated/anonymous status (no boot double-fetch race).
    if (status === null) {
        return (
            <div className="flex h-full items-center justify-center bg-background frost:bg-transparent">
                <span
                    className="fade-in animate-in text-muted-foreground/70 text-sm duration-500"
                    style={{ animationFillMode: "backwards" }}
                >
                    <LeapyLoading size={18} />
                </span>
            </div>
        );
    }

    // Anonymous visitors browse the shell as a public site (public_read mode);
    // operator routes redirect home before they mount. Nothing here gates on
    // authentication anymore.

    return (
        <IllustViewerProvider>
            {isMobile ? (
                <MobileShell>
                    <Outlet />
                </MobileShell>
            ) : (
                <div className="window-root-layout flex h-full overflow-hidden">
                    <ResizablePanelGroup className="min-w-0 flex-1" orientation="horizontal">
                        <ResizablePanel id="sidebar" defaultSize="14%" minSize="10%" maxSize="22%">
                            <RootSidebar />
                        </ResizablePanel>
                        <ResizableHandle className="window-sidebar-handle" />
                        <ResizablePanel id="main" className="window-main-panel">
                            {/* The ScrollArea viewport (not <main>) is the real page scroller — see
                                [data-app-scroller] consumers in settings scroll-spy + pager scroll-to-top. */}
                            <main className="window-main-surface h-full min-h-0 bg-background">
                                <ScrollArea className="h-full" viewportProps={{ "data-app-scroller": "" }}>
                                    <Outlet />
                                </ScrollArea>
                            </main>
                        </ResizablePanel>
                        <ActivityPanelSlot />
                    </ResizablePanelGroup>
                    <ActivityBar />
                </div>
            )}
        </IllustViewerProvider>
    );
}

export default RootLayout;
