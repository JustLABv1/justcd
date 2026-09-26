"use client"

import Link from "next/link"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { siGit, siKubernetes } from "simple-icons"
import { ThemePicker } from "@/components/theme-picker"
import styles from "./auth-shell.module.css"

function Cluster({ x, y, name }: { x: number; y: number; name: string }) {
  return <g transform={`translate(${x} ${y})`}>
    <rect x="-72" y="-60" width="144" height="120" rx="18" className={styles.node} />
    <svg x="-28" y="-33" width="56" height="56" viewBox="0 0 24 24" fill={`#${siKubernetes.hex}`}>
      <path d={siKubernetes.path} />
    </svg>
    <text y="45" textAnchor="middle">{name}</text>
    <circle cx="56" cy="-44" r="3" className={styles.clusterLight} />
  </g>
}

export function AuthShell({ eyebrow, title, description, children }: { eyebrow: string; title: string; description: string; children: ReactNode }) {
  const [paused, setPaused] = useState(false)
  const diagramRef = useRef<SVGSVGElement>(null)
  const cardRef = useRef<HTMLElement>(null)
  const [bounds, setBounds] = useState({ top: 130, bottom: 770 })

  useEffect(() => {
    const diagram = diagramRef.current
    const card = cardRef.current
    if (!diagram || !card) return
    const updateRoutes = () => {
      const matrix = diagram.getScreenCTM()
      if (!matrix || !diagram.getBoundingClientRect().width) return
      const rect = card.getBoundingClientRect()
      const inverse = matrix.inverse()
      // Keep outer connections clear of the form and its caption at every size.
      const top = Math.min(130, new DOMPoint(rect.left, rect.top - 40).matrixTransform(inverse).y)
      const bottom = Math.max(770, new DOMPoint(rect.left, rect.bottom + 72).matrixTransform(inverse).y)
      setBounds(previous => previous.top === top && previous.bottom === bottom ? previous : { top, bottom })
    }
    const observer = new ResizeObserver(updateRoutes)
    observer.observe(diagram)
    observer.observe(card)
    return () => observer.disconnect()
  }, [])

  const syncRoutes = [
    `M340 450Q388 450 388 402V${bounds.top + 48}Q388 ${bounds.top} 436 ${bounds.top}H968Q1016 ${bounds.top} 1016 ${bounds.top + 48}V232Q1016 280 1064 280H1108`,
    "M340 450H1108",
    `M340 450Q388 450 388 498V${bounds.bottom - 48}Q388 ${bounds.bottom} 436 ${bounds.bottom}H968Q1016 ${bounds.bottom} 1016 ${bounds.bottom - 48}V668Q1016 620 1064 620H1108`,
  ]
  return <main className={styles.scene} data-paused={paused}>
    <header className={styles.header}>
      <Link href="/login" className={styles.brand} aria-label="JustCD sign in"><span className={styles.logo}>J</span>JustCD<span className={styles.brandCaption}>continuous delivery</span></Link>
      <ThemePicker />
    </header>
    <div className={styles.backdrop} aria-hidden="true">
      <svg ref={diagramRef} viewBox="0 0 1440 900" fill="none" className={styles.diagram}>
        <g className={styles.routes}>
          <path d="M272 450H340" />
          {syncRoutes.map(route => <path key={route} d={route} />)}
        </g>
        <g className={styles.packets}>
          {syncRoutes.map(route => <path key={route} d={`M272 450H340${route.slice("M340 450".length)}`} />)}
        </g>
        <g transform="translate(200 450)">
          <rect x="-72" y="-70" width="144" height="140" rx="18" className={styles.node} />
          <svg x="-28" y="-33" width="56" height="56" viewBox="0 0 24 24" fill={`#${siGit.hex}`}>
            <path d={siGit.path} />
          </svg>
          <text y="53" textAnchor="middle">git / main</text>
        </g>
        <Cluster x={1180} y={280} name="cluster / eu" />
        <Cluster x={1180} y={450} name="cluster / us" />
        <Cluster x={1180} y={620} name="cluster / edge" />
        <text x="200" y="350" textAnchor="middle" className={styles.caption}>SOURCE OF TRUTH</text>
        <text x="1180" y="177" textAnchor="middle" className={styles.caption}>DESIRED STATE</text>
        <g className={styles.ports}>
          <circle cx="272" cy="450" r="3" />
          <circle cx="340" cy="450" r="3" />
          {[280, 450, 620].map(y => <circle key={y} cx="1108" cy={y} r="3" />)}
        </g>
      </svg>
    </div>
    <div className={styles.content}>
      <section ref={cardRef} className={styles.card} aria-labelledby="auth-title">
        <div className={styles.cardMark} aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5" /></svg></div>
        <p className={styles.eyebrow}>{eyebrow}</p>
        <h1 id="auth-title" className={styles.title}>{title}</h1>
        <p className={styles.description}>{description}</p>
        {children}
      </section>
      <p className={styles.tagline}>Your Git. Your clusters. In sync.</p>
    </div>
    <button type="button" className={styles.motionControl} onClick={() => setPaused(value => !value)} aria-pressed={paused} aria-label={paused ? "Resume background animation" : "Pause background animation"}>{paused ? "▷" : "Ⅱ"}<span>{paused ? "Resume animation" : "Pause animation"}</span></button>
  </main>
}
