import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/components/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        background: "var(--background)",
        foreground: "var(--foreground)",
        void: "var(--bg-void)",
        "bg-base": "var(--bg-base)",
        panel: "var(--bg-panel)",
        card: "var(--bg-card)",
        hover: "var(--bg-hover)",
        border: "var(--border)",
        "border-light": "var(--border-light)",
        primary: "var(--text-primary)",
        "text-secondary": "var(--text-secondary)",
        "text-dim": "var(--text-dim)",
        "accent-blue": "var(--accent-blue)",
        "accent-blue-dim": "var(--accent-blue-dim)",
        "accent-green": "var(--accent-green)",
        "accent-green-dim": "var(--accent-green-dim)",
        "accent-red": "var(--accent-red)",
        "accent-red-dim": "var(--accent-red-dim)",
        "accent-amber": "var(--accent-amber)",
        "accent-purple": "var(--accent-purple)",
      },
    },
  },
  plugins: [],
};
export default config;
