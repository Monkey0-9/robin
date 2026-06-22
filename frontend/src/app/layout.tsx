import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Robin Terminal",
  description: "Low-latency Trading Platform",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className="bg-void text-primary">{children}</body>
    </html>
  );
}
