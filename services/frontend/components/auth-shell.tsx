"use client"

import { Card } from "@/components/panelui/card"
import { DocumentationLink } from "@/components/documentation-link"
import Link from "next/link"
import type { ReactNode } from "react"
import { LogoMark } from "@/components/logo"
import { ThemePicker } from "@/components/theme-picker"
import styles from "./auth-shell.module.css"

export function AuthShell({ eyebrow, title, description, children }: { eyebrow: string; title: string; description: string; children: ReactNode }) {
  return <main className={styles.scene}>
    <header className={styles.header}>
      <Link href="/login" className={styles.brand} aria-label="JustCD sign in"><LogoMark className={styles.logo} />JustCD<span className={styles.brandCaption}>continuous delivery</span></Link>
      <div className="flex items-center gap-2"><DocumentationLink compact /><ThemePicker /></div>
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
      <Card as="section" className={styles.card} aria-labelledby="auth-title">
        <Card.Header>
        <p className={styles.eyebrow}>{eyebrow}</p>
        <h1 id="auth-title" className={styles.title}>{title}</h1>
        <Card.Description className={styles.description}>{description}</Card.Description>
        </Card.Header>
        <Card.Content>
        {children}
        </Card.Content>
      </Card>
      <p className={styles.tagline}>Your Git. Your clusters. In sync.</p>
    </div>
  </main>
}
