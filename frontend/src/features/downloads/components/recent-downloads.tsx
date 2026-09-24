import { NavLink } from "react-router";
import { Sheet, SheetBody, SheetEmpty, SheetHead } from "@/components/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useAuth } from "@/features/auth";
import { useDownloadCounts, useDownloadsPage } from "@/features/downloads";
import DownloadsTable from "@/features/downloads/components/downloads-table";
import { useMessages } from "@/i18n";
import { DownloadIcon } from "@/lib/icons";

const RECENT_LIMIT = 10;

// Recent downloads panel. Split into two components on purpose: the guest
// branch must never mount useDownloadsPage (the queue/history endpoints are
// operator-only and would 401 into a misleading empty table), and swapping
// component identity keeps each branch's hook list stable across an
// anonymous → login transition.
function RecentDownloads() {
    const { status } = useAuth();
    if (!status?.authenticated) return <RecentDownloadsGuest />;
    return <RecentDownloadsList />;
}

function RecentDownloadsGuest() {
    const m = useMessages();
    return (
        <Sheet>
            <SheetHead icon={DownloadIcon} title={m.downloads_title()} />
            <SheetBody>
                <SheetEmpty icon={DownloadIcon} title={m.downloads_guest_notice()} hint={m.downloads_guest_hint()} />
            </SheetBody>
        </Sheet>
    );
}

function RecentDownloadsList() {
    const m = useMessages();
    const { items, isLoading } = useDownloadsPage({ page: 1, perPage: RECENT_LIMIT });
    const { activeCount, doneCount } = useDownloadCounts();

    return (
        <Sheet>
            <SheetHead
                icon={DownloadIcon}
                title={m.downloads_title()}
                meta={m.downloads_recent_done_count({ active: activeCount, done: doneCount })}
                actions={
                    <NavLink to="/downloads">
                        <Button variant="ghost" size="sm">
                            {m.common_view_all()}
                        </Button>
                    </NavLink>
                }
            />
            <SheetBody>
                {/* While the first fetch is in flight render nothing (SheetBody holds the height)
                    so the empty state doesn't flash before data arrives. */}
                {isLoading ? null : items.length === 0 ? (
                    <SheetEmpty icon={DownloadIcon} title={m.downloads_empty_all()} hint={m.downloads_empty_hint()} />
                ) : (
                    <ScrollArea className="h-full">
                        <DownloadsTable jobs={items} compact />
                    </ScrollArea>
                )}
            </SheetBody>
        </Sheet>
    );
}

export default RecentDownloads;
