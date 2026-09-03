"use client";
import { FormEvent, KeyboardEvent, useState } from "react";
import { api, ApiRequestError } from "../lib/api";
import type { Question } from "../lib/types";
export default function QuestionForm({ onAnswered }: { onAnswered: (question: Question) => void }) {
  const [content, setContent] = useState(""); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent | KeyboardEvent<HTMLTextAreaElement>) { event.preventDefault(); if (!content.trim() || busy) return; setBusy(true); setError(""); try { onAnswered(await api.createQuestion(content.trim())); setContent(""); } catch (e) { setError(e instanceof ApiRequestError ? e.message : "提问失败，请稍后重试"); } finally { setBusy(false); } }
  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void submit(event); } }
  return <form onSubmit={submit} className="repl-window"><div className="window-bar"><span>● ● ●</span><span>cs-qa</span></div><div className="repl-input-row"><span aria-hidden="true">❯</span><textarea aria-label="输入你的问题" value={content} maxLength={2000} onChange={(e) => setContent(e.target.value)} onKeyDown={handleKeyDown} placeholder={busy ? "正在编译你的问题…" : "输入你的问题，按 Enter 发送；Shift+Enter 换行"} disabled={busy} rows={3} /></div>{error && <p role="alert">{error}</p>}</form>;
}
