import { readdir, readFile, access } from "node:fs/promises";
import path from "node:path";

const root = path.resolve("content/docs");
async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map(async (entry) => {
        const file = path.join(directory, entry.name);
        return entry.isDirectory() ? walk(file) : [file];
      }),
    )
  ).flat();
}
function routeFor(file) {
  return (
    "/" +
    path
      .relative(root, file)
      .replace(/\.(md|mdx)$/, "")
      .replace(/(^|\/)index$/, "")
  );
}
function slug(value) {
  return value
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, "")
    .replace(/ /g, "-");
}
function prose(content) {
  return content
    .replace(/^---\n[\s\S]*?\n---\n/, "")
    .replace(/^(`{3,}|~{3,})[^\n]*\n[\s\S]*?^\1\s*$/gm, "");
}
const files = await walk(root);
const pages = new Map();
for (const file of files.filter((file) => /\.(md|mdx)$/.test(file))) {
  const content = prose(await readFile(file, "utf8"));
  const headings = new Set();
  for (const match of content.matchAll(/^#{1,6}\s+(.+)$/gm)) {
    const base = slug(match[1]);
    let anchor = base;
    for (let i = 1; headings.has(anchor); i++) anchor = `${base}-${i}`;
    headings.add(anchor);
  }
  pages.set(routeFor(file), { file, content, headings });
}
const failures = [];
for (const [route, page] of pages) {
  for (const match of page.content.matchAll(
    /\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g,
  )) {
    const target = match[1];
    if (/^(https?:|mailto:)/.test(target)) continue;
    if (!target.startsWith("/") && !target.startsWith("#")) {
      failures.push(
        `${route}: use a site route or absolute URL instead of ${target}`,
      );
      continue;
    }
    const [pathname, fragment] = target.split("#");
    const destination = pages.get(pathname || route);
    if (destination) {
      if (fragment && !destination.headings.has(decodeURIComponent(fragment)))
        failures.push(`${route}: missing heading ${target}`);
    } else {
      try {
        await access(path.join("public", pathname));
      } catch {
        failures.push(`${route}: missing page or public asset ${target}`);
      }
    }
  }
}
for (const file of files.filter(
  (file) => path.basename(file) === "meta.json",
)) {
  const meta = JSON.parse(await readFile(file, "utf8"));
  for (const name of meta.pages ?? []) {
    const base = path.join(path.dirname(file), name);
    if (
      ![base + ".md", base + ".mdx", path.join(base, "meta.json")].some(
        (candidate) => files.includes(candidate),
      )
    )
      failures.push(`${file}: missing navigation entry ${name}`);
  }
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exitCode = 1;
} else
  console.log(
    `Validated ${pages.size} documentation pages, internal links, headings, and navigation.`,
  );
