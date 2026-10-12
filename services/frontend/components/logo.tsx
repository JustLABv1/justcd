export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="28 28 200 200" role="img" aria-label="JustCD" className={className}>
      <path
        d="M149.7 46.9A84 84 0 1 0 211.7 120.7"
        fill="none"
        stroke="currentColor"
        strokeWidth="32"
        strokeLinecap="round"
      />
      <circle cx="192.4" cy="74" r="19" fill="var(--primary)" />
    </svg>
  )
}
