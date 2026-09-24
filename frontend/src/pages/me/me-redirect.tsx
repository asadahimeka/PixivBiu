import { Navigate, useLocation, useParams } from "react-router";
import { useAuth } from "@/features/auth";
import { normalizeTab } from "@/pages/user/tabs";

function MeRedirect() {
    const { status } = useAuth();
    const location = useLocation();
    const { tab: rawTab } = useParams<{ tab?: string }>();

    if (status === null) return null;
    // /me is the operator's own profile. Public mode closed /login (the route
    // is hidden), so a guest hitting /me goes home — never at a dead target.
    // Local mode keeps the login bounce with its return path; an authenticated
    // session missing user_id can't build a /user/… URL, so it falls back to
    // home like before.
    if (!status.authenticated) {
        if (status.public_read) return <Navigate replace to="/" />;
        return <Navigate replace to="/login" state={{ from: location }} />;
    }
    if (!status.user_id) return <Navigate replace to="/" />;

    const tab = normalizeTab(rawTab);

    return <Navigate replace to={`/user/${status.user_id}?tab=${tab}`} />;
}

export default MeRedirect;
