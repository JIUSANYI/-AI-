import "./globals.css";

export const metadata = {
  title: "cs·qa | 把问题交给终端",
  description: "面向计算机学习者的 AI 问答平台"
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}

