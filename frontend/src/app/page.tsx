"use client";
import { useState } from "react";
import Link from "next/link";
import { useAuth } from "../context/AuthContext";
import QuestionForm from "../components/QuestionForm";
import QuestionAnswer from "../components/QuestionAnswer";
import type { Question } from "../lib/types";

export default function HomePage() {
  const { authenticated, loading, logout } = useAuth(); const [question, setQuestion] = useState<Question | null>(null);
  if (loading) return <main className="reading-column">正在加载…</main>;
  return (
    <main className="page-shell">
      <header className="topbar">
        <span className="logo">◉ cs·qa</span>
        <nav aria-label="主导航">
          <Link href="/">提问</Link>
          <Link href="/history">历史</Link>
        </nav>
      </header>
      <section className="reading-column">
        <p className="eyebrow">COMPUTER SCIENCE / QUESTION DESK</p>
        <h1>把问题交给终端。</h1>
        {authenticated ? <QuestionForm onAnswered={setQuestion} /> : <div className="repl-window" aria-label="提问窗口"><div className="window-bar"><span>● ● ●</span><span>cs-qa</span></div><div className="repl-input-row"><Link href="/login">登录后开始提问</Link></div></div>}
        {question ? <QuestionAnswer question={question} /> : <div className="paper-card"><p className="empty-prompt">还没有提问记录。回到终端，问出第一个问题。</p></div>}
        {authenticated && <button type="button" onClick={() => logout()}>退出登录</button>}
      </section>
    </main>
  );
}
