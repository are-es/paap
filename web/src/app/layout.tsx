import type { Metadata } from "next";
import { Space_Grotesk, JetBrains_Mono } from "next/font/google";
import localFont from "next/font/local";
import { Sidebar } from "@/components/sidebar";
import { Providers } from "@/components/providers";
import { ThemeProvider } from "@/components/theme-provider";
import { LanguageProvider } from "@/lib/language-context";
import "./globals.css";

const bricolage = localFont({
  src: [
    {
      path: "../../public/assets/fonts/BricolageGrotesque-Bold.ttf",
      weight: "700",
      style: "normal",
    },
  ],
  variable: "--font-heading",
  display: "swap",
});

const spaceGrotesk = Space_Grotesk({
  variable: "--font-sans",
  subsets: ["latin"],
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
  display: "swap",
});

export const metadata: Metadata = {
  title: "PAAP — Pangkalan API",
  description: "AI Proxy/Router Dashboard",
  icons: { icon: "/assets/favicon.svg" },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      data-theme="light"
      suppressHydrationWarning
      className={`${bricolage.variable} ${spaceGrotesk.variable} ${jetbrainsMono.variable} h-full antialiased`}
    >
      <body className="h-screen overflow-hidden flex font-sans">
        <ThemeProvider>
          <LanguageProvider>
            <Providers>
              <Sidebar />
              <main className="flex-1 overflow-auto">{children}</main>
            </Providers>
          </LanguageProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
