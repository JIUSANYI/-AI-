"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "../context/AuthContext";

export default function AppHeader() {
  const pathname = usePathname();
  const { authenticated, loading, logout } = useAuth();

  function current(href: string) {
    if (href === "/history" && (pathname === "/history" || pathname.startsWith("/questions/"))) return "page";
    return pathname === href ? "page" : undefined;
  }

  return (
    <header className="topbar">
      <Link className="logo" href="/" aria-label="cs·qa，返回首页">◉ cs·qa</Link>
      <nav aria-label="主导航">
        <Link aria-current={current("/")} href="/">提问</Link>
        <Link aria-current={current("/history")} href="/history">历史</Link>
        {loading ? <span className="nav-loading" aria-hidden="true" /> : (authenticated ? <button type="button" onClick={() => void logout()}>退出登录</button> : <Link aria-current={current("/login")} href="/login">登录</Link>)}
      </nav>
    </header>
  );
}
