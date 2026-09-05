"use client";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { api, refreshAccessToken, setAccessToken } from "../lib/api";
import type { User } from "../lib/types";
type AuthContextValue = { user: User | null; authenticated: boolean; loading: boolean; login: (phone: string, code: string) => Promise<void>; logout: () => Promise<void> };
const AuthContext = createContext<AuthContextValue | null>(null);
export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null); const [authenticated, setAuthenticated] = useState(false); const [loading, setLoading] = useState(true);
  useEffect(() => {
    refreshAccessToken().then(async (token) => {
      if (!token) return;
      try { setUser(await api.me()); setAuthenticated(true); }
      catch { setAccessToken(null); setAuthenticated(false); }
    }).finally(() => setLoading(false));
  }, []);
  const value = useMemo<AuthContextValue>(() => ({ user, authenticated, loading, login: async (phone, code) => { const data = await api.login(phone, code); setAccessToken(data.access_token); setUser(data.user); setAuthenticated(true); }, logout: async () => { try { await api.logout(); } finally { setAccessToken(null); setUser(null); setAuthenticated(false); } } }), [user, authenticated, loading]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
export function useAuth() { const ctx = useContext(AuthContext); if (!ctx) throw new Error("useAuth 必须在 AuthProvider 内使用"); return ctx; }
