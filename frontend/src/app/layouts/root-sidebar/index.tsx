import { AccountButton, useAuth } from "@/features/auth";
import Nav from "./nav";
import { SidebarFooter } from "./sidebar-footer";

function RootSidebar() {
    const { status } = useAuth();
    const publicMode = status?.public_read === true;
    return (
        // pt: 1rem in the browser; on the macOS frameless shell the wordmark
        // drops below the traffic lights (--traffic-lights-inset, desktop.css).
        // Empty space drags the desktop window; the sidebar's own controls
        // opt out in desktop.css. The marker also enables dragging on Windows.
        <aside
            data-window-sidebar=""
            className="app-drag flex h-full flex-col gap-4 bg-sidebar px-3 pt-[max(1rem,var(--traffic-lights-inset,0px))] pb-3"
        >
            <div className="flex items-center gap-1.5 px-2 pt-1 pb-3">
                <div className="font-medium text-foreground text-xl">PixivBiu</div>
            </div>

            <Nav />

            <div className="flex-1" />

            <div className="flex flex-col gap-3">{publicMode ? <SidebarFooter /> : <AccountButton />}</div>
        </aside>
    );
}

export default RootSidebar;
