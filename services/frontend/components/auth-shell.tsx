"use client"

import Link from "next/link"
import type { ReactNode } from "react"
import { ThemePicker } from "@/components/theme-picker"
import styles from "./auth-shell.module.css"

export function AuthShell({ eyebrow, title, description, children }: { eyebrow: string; title: string; description: string; children: ReactNode }) {
  return <main className={styles.scene}>
    <header className={styles.header}>
      <Link href="/login" className={styles.brand} aria-label="JustCD sign in"><span className={styles.logo}>J</span>JustCD<span className={styles.brandCaption}>continuous delivery</span></Link>
      <ThemePicker />
    </header>
    <div className={styles.backdrop} aria-hidden="true">
      <div className={styles.atmosphere} />
      <div className={`${styles.layers} ${styles.layersLeft}`}>
        <div className={styles.plane} /><div className={styles.plane} /><div className={styles.plane} />
      </div>
      <div className={`${styles.layers} ${styles.layersRight}`}>
        <div className={styles.plane} /><div className={styles.plane} /><div className={styles.plane} />
      </div>
    </div>
    <div className={styles.content}>
      <section className={styles.card} aria-labelledby="auth-title">
        <div className={styles.cardMark} aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5" /></svg></div>
        <p className={styles.eyebrow}>{eyebrow}</p>
        <h1 id="auth-title" className={styles.title}>{title}</h1>
        <p className={styles.description}>{description}</p>
        {children}
      </section>
      <p className={styles.tagline}>Your Git. Your clusters. In sync.</p>
    </div>
  </main>
}
