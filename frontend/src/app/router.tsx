import { createBrowserRouter, Navigate } from "react-router";
import RootLayout from "@/app/layouts/root-layout";
import { useAuth } from "@/features/auth";
import DownloadsPage from "@/pages/downloads";
import Home from "@/pages/home";
import LoginPage from "@/pages/login";
import MeRedirect from "@/pages/me/me-redirect";
import RankingPage from "@/pages/ranking";
import SearchPage from "@/pages/search";
import SettingsPage from "@/pages/settings";
import UserPage from "@/pages/user";

// Public mode closed the auth surface server-side (every user-login operation
// answers 404), so /login resolves to home instead of a form that can never
// submit. The shared status arrives after mount: while it's unknown, render
// nothing — guessing would flash the form once in public mode, which the
// "never the form" rule forbids; local mode pays one blank frame before the
// exact same login page.
function LoginRoute() {
    const { status } = useAuth();
    if (status === null) return null;
    if (status.public_read) return <Navigate to="/" replace />;
    return <LoginPage />;
}

export const router = createBrowserRouter([
    { path: "/login", element: <LoginRoute /> },
    {
        path: "/",
        element: <RootLayout />,
        children: [
            { index: true, element: <Home /> },
            { path: "search", element: <SearchPage /> },
            { path: "search/:keyword", element: <SearchPage /> },
            { path: "ranking", element: <RankingPage /> },
            { path: "user/:id", element: <UserPage /> },
            { path: "me/:tab?", element: <MeRedirect /> },
            { path: "downloads", element: <DownloadsPage /> },
            { path: "settings", element: <SettingsPage /> },
        ],
    },
]);
