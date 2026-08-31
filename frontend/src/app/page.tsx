export default function HomePage() {
  return (
    <main className="page-shell">
      <header className="topbar">
        <span className="logo">◉ cs·qa</span>
        <nav aria-label="主导航">
          <a href="/">提问</a>
          <a href="/history">历史</a>
        </nav>
      </header>
      <section className="reading-column">
        <p className="eyebrow">COMPUTER SCIENCE / QUESTION DESK</p>
        <h1>把问题交给终端。</h1>
        <div className="repl-window" aria-label="提问窗口">
          <div className="window-bar"><span>● ● ●</span><span>cs-qa</span></div>
          <div className="repl-input-row">
            <span aria-hidden="true">❯</span>
            <input aria-label="输入你的问题" placeholder="输入你的问题，按 Enter 发送" />
          </div>
        </div>
        <div className="paper-card">
          <p className="empty-prompt">还没有提问记录。回到终端，问出第一个问题。</p>
        </div>
      </section>
    </main>
  );
}

