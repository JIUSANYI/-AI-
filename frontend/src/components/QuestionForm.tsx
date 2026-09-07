"use client";
import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from "react";
import { api, ApiRequestError } from "../lib/api";
import type { Question } from "../lib/types";
export default function QuestionForm({ onAnswered, initialValue = "" }: { onAnswered: (question: Question) => void; initialValue?: string }) {
  const [content, setContent] = useState(initialValue); const [busy, setBusy] = useState(false); const [error, setError] = useState(""); const inputRef = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (initialValue) { setContent(initialValue); inputRef.current?.focus(); } }, [initialValue]);
  async function submit(event: FormEvent | KeyboardEvent<HTMLTextAreaElement>) { event.preventDefault(); if (!content.trim() || busy) return; setBusy(true); setError(""); try { onAnswered(await api.createQuestion(content.trim())); setContent(""); } catch (e) { setError(e instanceof ApiRequestError ? e.message : "提问失败，请稍后重试"); } finally { setBusy(false); } }
  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void submit(event); } }
  return <form onSubmit={submit} className="repl-window" aria-busy={busy}><div className="window-bar"><span>● ● ●</span><span>cs-qa / ask</span></div><div className="repl-input-row"><span aria-hidden="true">❯</span><textarea ref={inputRef} aria-label="输入你的问题" aria-describedby="question-counter" value={content} maxLength={2000} onChange={(e) => setContent(e.target.value)} onKeyDown={handleKeyDown} placeholder={busy ? "正在编译你的问题…" : "输入你的问题，按 Enter 发送；Shift+Enter 换行"} disabled={busy} rows={3} /></div><div className="form-footer"><span id="question-counter" aria-live="polite">{content.length} / 2000</span><button type="submit" disabled={busy || !content.trim()}>{busy ? "处理中…" : "发送问题"}</button></div>{error && <p className="form-error" role="alert">{error}</p>}</form>;
}
