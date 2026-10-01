import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Dendrite — Local-First Knowledge Graph",
  description:
    "Markdown notes with an automatically generated knowledge graph.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
