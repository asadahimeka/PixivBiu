import { useCallback, useState } from "react";
import { useAuth } from "@/features/auth";
import RecentDownloads from "@/features/downloads/components/recent-downloads";
import SearchBar from "@/features/search/components/search-bar";
import FollowedAuthors from "@/features/users/components/followed-authors";
import HomeIllustTabs, { type TabId } from "./components/illust-tabs";
import { useGreeting } from "./use-greeting";

function Home() {
    const greeting = useGreeting();
    const { status } = useAuth();
    const [activeTab, setActiveTab] = useState<TabId>("for-you");
    // Public mode hides the RecentDownloads block (an operator-only surface);
    // the section then drops its two-column grid so the FollowedAuthors panel
    // doesn't sit beside an empty column.
    const publicMode = status?.public_read === true;

    const handleViewFollow = useCallback(() => {
        setActiveTab("follow");
    }, []);

    return (
        <div className="relative flex flex-col gap-6 px-7 pt-4 pb-7">
            <SearchBar />
            <h1 className="m-0 font-normal text-3xl text-foreground leading-tight">{greeting}</h1>
            <section className={publicMode ? "items-start" : "grid grid-cols-[1.3fr_1fr] items-start gap-4"}>
                {!publicMode && <RecentDownloads />}
                <FollowedAuthors onView={handleViewFollow} />
            </section>
            <HomeIllustTabs activeTab={activeTab} onActiveTabChange={setActiveTab} />
        </div>
    );
}

export default Home;
