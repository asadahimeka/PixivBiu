// Auth scope for identity-dependent query keys. The reset-on-logout guard in
// <AuthGatedQueryReset> only clears the cache when LEAVING an authenticated
// session, so an anonymous → login transition would otherwise reuse lists
// fetched through the pool identity (recommendations/follows are per-account).
// Prefixing those keys with the scope makes login land on fresh keys while
// logout still gets its full clear.
export type AuthQueryScope = "user" | "anonymous";

export function authQueryScope(authenticated: boolean | undefined | null): AuthQueryScope {
    return authenticated ? "user" : "anonymous";
}
