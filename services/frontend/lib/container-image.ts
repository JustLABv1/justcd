/** Splits an image reference into its repository and the part that identifies the version. */
export function parseImage(image: string): { repository: string; version: string; digest: boolean } {
  const at = image.indexOf("@")
  if (at >= 0) return { repository: image.slice(0, at), version: image.slice(at + 1).replace(/^(sha256:)(.{12}).*$/, "$1$2"), digest: true }
  const lastSlash = image.lastIndexOf("/")
  const colon = image.indexOf(":", lastSlash + 1)
  if (colon < 0) return { repository: image, version: "latest", digest: false }
  return { repository: image.slice(0, colon), version: image.slice(colon + 1), digest: false }
}
