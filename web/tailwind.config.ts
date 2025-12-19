import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: ["class"],
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}"
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ["var(--font-sans)", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "ui-monospace", "SFMono-Regular"],
      },
      colors: {
        panel: "#0f172a",
        panelBorder: "#1e293b",
        accent: "#38bdf8",
        accentMuted: "#0ea5e9",
      },
      boxShadow: {
        panel: "0 0 0 1px rgba(148, 163, 184, 0.08), 0 18px 40px rgba(15, 23, 42, 0.45)",
      }
    },
  },
  plugins: [],
};

export default config;
