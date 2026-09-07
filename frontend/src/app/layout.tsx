import "./globals.css";
import AppHeader from "../components/AppHeader";
import { AuthProvider } from "../context/AuthContext";

export const metadata = {
  title: "cs·qa | 把问题交给终端",
  description: "面向计算机学习者的 AI 问答平台"
};

export const viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover" as const,
  colorScheme: "light" as const
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body><AuthProvider><a className="skip-link" href="#main-content">跳到主要内容</a><AppHeader />{children}</AuthProvider></body>
    </html>
  );
}
