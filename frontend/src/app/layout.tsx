import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
  weight: ["300", "400", "500", "600", "700", "800", "900"],
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  display: "swap",
  weight: ["400", "500", "600", "700"],
});

export const metadata: Metadata = {
  title: "Robin PRO — Institutional Trading Terminal",
  description:
    "Ultra-low latency quantitative trading platform with real-time risk management, Black-Scholes options pricing, AI signal engine, and institutional compliance tooling.",
  keywords: [
    "trading terminal", "algorithmic trading", "options chain",
    "risk management", "quantitative finance", "HFT",
  ],
  authors: [{ name: "Robin Trading Systems" }],
  robots: "noindex,nofollow", // Internal institutional tool — do not index
};

export const viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: "#050507",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className={`${inter.variable} ${jetbrainsMono.variable}`}>
      <body className="bg-void text-primary antialiased">
        {children}
      </body>
    </html>
  );
}
