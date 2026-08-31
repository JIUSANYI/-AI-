import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./src/**/*.{js,ts,jsx,tsx,mdx}"],
  theme: {
    extend: {
      colors: {
        paper: "var(--paper)",
        ink: "var(--ink)",
        circuit: "var(--circuit)",
        pass: "var(--pass)",
        signal: "var(--signal)",
        comment: "var(--comment)"
      }
    }
  },
  plugins: []
};

export default config;

