import "./globals.css";
import Link from "next/link";
import { IBM_Plex_Mono, Space_Grotesk } from "next/font/google";

import VersionFooter from "@/components/version-footer";

const sans = Space_Grotesk({
  subsets: ["latin"],
  display: "swap",
  variable: "--font-sans",
});

const mono = IBM_Plex_Mono({
  subsets: ["latin"],
  display: "swap",
  variable: "--font-mono",
  weight: ["400", "500", "600"],
});

export const metadata = {
  title: "Alert Framework CAD",
  description: "Sussex County CAD console",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`dark ${sans.variable} ${mono.variable}`}>
      <body>
        <div className="min-h-screen">
          <header className="border-b border-panelBorder bg-slate-950/60 px-6 py-4">
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <p className="text-xs uppercase tracking-[0.2em] text-slate-400">
                  Alert Framework
                </p>
                <h1 className="text-2xl font-semibold text-slate-100">
                  Sussex CAD Console
                </h1>
              </div>
              <nav className="flex items-center gap-4 text-sm text-slate-300">
                <Link className="hover:text-white" href="/calls">
                  Calls
                </Link>
                <Link className="hover:text-white" href="/alerts">
                  Alerts
                </Link>
                <Link className="hover:text-white" href="/tasks">
                  Tasks
                </Link>
                <Link className="hover:text-white" href="/settings">
                  Settings
                </Link>
              </nav>
            </div>
          </header>
          <main className="px-6 py-6">{children}</main>
          <VersionFooter />
        </div>
      </body>
    </html>
  );
}
