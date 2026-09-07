"use client";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main id="main-content" tabIndex={-1} className="reading-column">
      <p className="eyebrow">500 / RECOVERY</p>
      <h1>页面暂时没有响应。</h1>
      <p className="intro">刚才的操作没有完成，保留当前页面并重新尝试即可。</p>
      <div className="auth-gate">
        <p>重新加载当前页面内容。</p>
        <button type="button" onClick={reset}>重新尝试</button>
      </div>
    </main>
  );
}
