import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthApiError, AuthStatus } from "./api";
import * as authApi from "./api";
import { AuthContext, type AuthContextValue } from "./auth-context";

// Hydrates the first render from the last resolved status so a repeat visit
// skips the splash and the /auth/status round-trip entirely (the fetch below
// still runs and self-corrects stale cache). Only the public-mode branch
// benefits structurally — public_read never flips for a visitor — but local
// mode boots faster the same way. Cache misses (first visit / cleared
// storage) behave exactly as before.
const STATUS_CACHE_KEY = "pixivbiu.auth-status";

function readStatusCache(): AuthStatus | null {
    try {
        const raw = localStorage.getItem(STATUS_CACHE_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as AuthStatus;
        if (typeof parsed.authenticated !== "boolean") return null;
        return parsed;
    } catch {
        return null;
    }
}

function writeStatusCache(status: AuthStatus) {
    try {
        localStorage.setItem(STATUS_CACHE_KEY, JSON.stringify(status));
    } catch {
        // storage unavailable (private mode) — cache is best-effort
    }
}

export function AuthProvider({ children }: { children: ReactNode }) {
    const [status, setStatus] = useState<AuthStatus | null>(() => readStatusCache());
    const [pending, setPending] = useState(false);

    const refresh = useCallback(async () => {
        const { data, error } = await authApi.getAuthStatus();
        if (error) return error;
        if (data) writeStatusCache(data);
        setStatus(data);
        return null;
    }, []);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const login = useCallback(async (refreshToken: string) => {
        const trimmed = refreshToken.trim();
        if (!trimmed) {
            return { code: "bad_request", kind: "app", message: "Refresh token is required" } satisfies AuthApiError;
        }
        setPending(true);
        const { data, error } = await authApi.login(trimmed);
        setPending(false);
        if (error) return error;
        if (data) writeStatusCache(data);
        setStatus(data);
        return null;
    }, []);

    const logout = useCallback(async () => {
        setPending(true);
        const { error } = await authApi.logout();
        setPending(false);
        if (error) return error;
        return await refresh();
    }, [refresh]);

    const startOAuth = useCallback(async () => {
        setPending(true);
        const result = await authApi.startOAuth();
        setPending(false);
        return result;
    }, []);

    const exchangeOAuth = useCallback(async (state: string, code: string) => {
        const trimmedState = state.trim();
        const trimmedCode = code.trim();
        if (!trimmedState || !trimmedCode) {
            return { code: "bad_request", kind: "app", message: "State and code are required" } satisfies AuthApiError;
        }
        setPending(true);
        const { data, error } = await authApi.exchangeOAuth(trimmedState, trimmedCode);
        setPending(false);
        if (error) return error;
        if (data) writeStatusCache(data);
        setStatus(data);
        return null;
    }, []);

    const value = useMemo<AuthContextValue>(
        () => ({ status, pending, refresh, login, logout, startOAuth, exchangeOAuth }),
        [status, pending, refresh, login, logout, startOAuth, exchangeOAuth],
    );

    return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
