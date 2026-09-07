"use client";
import { useState } from "react";
import Link from "next/link";
import { useAuth } from "../context/AuthContext";
import QuestionForm from "../components/QuestionForm";
import QuestionAnswer from "../components/QuestionAnswer";
import type { Question } from "../lib/types";

export default function HomePage() {
  const { authenticated, loading } = useAuth(); const [question, setQuestion] = useState<Question | null>(null); const [suggestion, setSuggestion] = useState("");
  if (loading) return <main id="main-content" tabIndex={-1} className="reading-column"><div className="loading-state" aria-live="polite">正在加载<span className="loading-dots" aria-hidden="true"><i /><i /><i /></span></div></main>;
  return (
    <main id="main-content" tabIndex={-1} className="page-shell">
      <section className="reading-column">
        <p className="eyebrow">COMPUTER SCIENCE / QUESTION DESK</p>
        <h1>把问题交给终端。</h1>
        <p className="intro">面向计算机学习的 AI 问答桌面。输入一个具体问题，获得可读、可追溯的解释。</p>
        {authenticated ? <QuestionForm onAnswered={setQuestion} initialValue={suggestion} /> : <div className="repl-window" aria-label="提问窗口"><div className="window-bar"><span>● ● ●</span><span>cs-qa</span></div><div className="repl-input-row"><Link href="/login">登录后开始提问</Link></div></div>}
        {question ? <QuestionAnswer question={question} onUpdated={setQuestion} /> : <div className="paper-card empty-state"><p className="eyebrow">READY WHEN YOU ARE</p><p className="empty-prompt">从一个具体问题开始：解释概念、排查报错，或梳理学习路径。</p><div className="suggestions" aria-label="问题示例">{authenticated ? <><button type="button" onClick={() => setSuggestion("什么是 TCP 三次握手？")}>什么是 TCP 三次握手？</button><button type="button" onClick={() => setSuggestion("如何定位 Go 的内存泄漏？")}>如何定位 Go 的内存泄漏？</button><button type="button" onClick={() => setSuggestion("解释 React Server Components")}>解释 React Server Components</button></> : <Link className="suggestion-link" href="/login">登录后查看示例问题</Link>}</div></div>}
      </section>
    </main>
  );
}
