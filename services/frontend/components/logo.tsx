export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="6 3 82 86" role="img" aria-label="JustCD" className={className}>
      <circle cx="40" cy="16" r="9" fill="currentColor" />
      <g fill="none" stroke="currentColor" strokeWidth="9" strokeLinecap="round" strokeLinejoin="round">
        <path d="M40 38V66a16 16 0 0 1-16 16H14" />
        <path d="M40 62C40 44 72 48 72 32" />
        <circle cx="72" cy="18" r="9" strokeWidth="8" />
      </g>
    </svg>
  )
}
