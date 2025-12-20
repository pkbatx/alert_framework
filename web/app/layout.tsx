import "./globals.css";
import Link from "next/link";

import HeaderStatus from "@/components/header-status";
import VersionFooter from "@/components/version-footer";

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
    <html lang="en" className="dark">
      <body>
        <div className="min-h-screen">
          <header className="border-b border-panelBorder bg-slate-950/60 px-6 py-4">
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div className="flex items-center gap-4">
                <img
                  src="/caad-logo.svg"
                  alt="CAAD logo"
                  className="h-10 w-auto"
                />
                <div>
                  <p className="text-xs uppercase tracking-[0.2em] text-slate-400">
                    Computer Aided Agent Dispatch
                  </p>
                  <h1 className="text-2xl font-semibold text-slate-100">
                    Sussex County CAD
                  </h1>
                </div>
              </div>
              <HeaderStatus />
              <nav className="flex items-center gap-4 text-sm text-slate-300">
                <Link className="hover:text-white" href="/calls">
                  Calls
                </Link>
                <Link className="hover:text-white" href="/alerts">
                  Alerts
                </Link>
                <Link className="hover:text-white" href="/rollups">
                  Rollups
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
