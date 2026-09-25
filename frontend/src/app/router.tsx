import { lazy, type ReactElement, type ReactNode, Suspense } from "react";
import { createBrowserRouter, Navigate } from "react-router";
import RootLayout from "@/app/layouts/root-layout";
import { useAuth } from "@/features/auth";
import Home from "@/pages/home";
import RankingPage from "@/pages/ranking";
import SearchPage from "@/pages/search";
import UserPage from "@/pages/user";

// Operator surfaces are code-split: a purely anonymous public site never
// downloads their JS, while local mode fetches them on first navigation.
const LoginPage = lazy(() => import("@/pages/login"));
const MeRedirect = lazy(() => import("@/pages/me/me-redirect"));
const DownloadsPage = lazy(() => import("@/pages/downloads"));
const SettingsPage = lazy(() => import("@/pages/settings"));

// Public mode closed the auth/settings/downloads surfaces server-side; direct
// URLs resolve to home instead of a guest notice or a form that can never
// submit. The shared status arrives after mount: while it's unknown, render
// nothing — guessing would flash the form once in public mode, which the
// "never the form" rule forbids; local mode pays one blank frame before the
// exact same page.
function PublicRedirect({ children }: { children: ReactNode }) {
    const { status } = useAuth();
    if (status === null) return null;
    if (status.public_read) return <Navigate to="/" replace />;
    return children as ReactElement;
}

function guarded(element: ReactNode): ReactElement {
    return <PublicRedirect>{element}</PublicRedirect>;
}

function lazyPage(page: ReactElement): ReactElement {
    return <Suspense fallback={null}>{page}</Suspense>;
}

export const router = createBrowserRouter([
    { path: "/login", element: guarded(lazyPage(<LoginPage />)) },
    {
        path: "/",
        element: <RootLayout />,
        children: [
            { index: true, element: <Home /> },
            { path: "search", element: <SearchPage /> },
            { path: "search/:keyword", element: <SearchPage /> },
            { path: "ranking", element: <RankingPage /> },
            { path: "user/:id", element: <UserPage /> },
            { path: "me/:tab?", element: guarded(lazyPage(<MeRedirect />)) },
            { path: "downloads", element: guarded(lazyPage(<DownloadsPage />)) },
            { path: "settings", element: guarded(lazyPage(<SettingsPage />)) },
        ],
    },
]);
