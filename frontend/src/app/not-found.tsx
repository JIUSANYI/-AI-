import Link from "next/link";

export default function NotFound() {
  return (
    <main id="main-content" tabIndex={-1} className="reading-column">
      <p className="eyebrow">404 / NOT FOUND</p>
      <h1>这条路径没有结果。</h1>
      <p className="intro">链接可能已经失效，或者这条问答还没有被创建。</p>
      <div className="auth-gate">
        <p>回到提问页，重新开始一次查询。</p>
        <Link href="/">返回提问</Link>
      </div>
    </main>
  );
}
