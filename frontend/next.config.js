/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  distDir: process.env.NEXT_DIST_DIR || ".next",
  reactStrictMode: true,
  async rewrites() {
    const backend = process.env.BACKEND_INTERNAL_URL ||
      (process.env.NODE_ENV === "production" ? "http://backend:8080" : "http://localhost:8080");
    return [{ source: "/api/v1/:path*", destination: `${backend}/api/v1/:path*` }];
  }
};

module.exports = nextConfig;
