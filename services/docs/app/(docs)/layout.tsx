import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { source } from "@/lib/source";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DocsLayout
      tree={source.getPageTree()}
      nav={{ title: "JustCD / Docs", url: "/" }}
      githubUrl="https://github.com/JustLABv1/justcd"
    >
      {children}
    </DocsLayout>
  );
}
