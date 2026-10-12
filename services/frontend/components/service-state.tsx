"use client"

import Link from "next/link"
import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { LogoMark } from "@/components/logo"
import { ThemePicker } from "@/components/theme-picker"
import styles from "./service-state.module.css"

export type ServiceState = "maintenance" | "not-found" | "offline"

const content = {
  maintenance: {
    code: "A little scheduled care",
    title: "Making room for better.",
    description:
      "JustCD is taking a maintenance break. Please check back shortly to continue your work.",
    caption: "Fine-tuning the delivery engine",
  },
  "not-found": {
    code: "404 · Uncharted territory",
    title: "This route drifted away.",
    description:
      "We couldn’t find the page you’re looking for. The link may have changed, or the page may no longer exist.",
    caption: "Let’s get you back into orbit",
  },
  offline: {
    code: "Connection interrupted",
    title: "Waiting for a signal.",
    description:
      "We can’t reach the JustCD backend right now. We’ll keep checking and reconnect when it’s available.",
    caption: "Listening for the delivery engine",
  },
}

function StateArtwork({ state }: { state: ServiceState }) {
  return (
    <div className={`${styles.artwork} ${styles[state]}`} aria-hidden="true">
      <div className={styles.halo} />
      <div className={styles.grid} />
      {state === "not-found" ? (
        <>
          <div className={styles.orbit}>
            <span />
          </div>
          <div className={`${styles.orbit} ${styles.orbitTwo}`}>
            <span />
          </div>
          <div className={styles.coordinates}>ROUTE / UNKNOWN</div>
          <div className={styles.numerals}>
            4<span>0</span>4
          </div>
          <div className={styles.satellite}>
            <LogoMark className="size-7" />
          </div>
        </>
      ) : state === "maintenance" ? (
        <>
          <div className={styles.assembly}>
            {[0, 1, 2].map((layer) => (
              <div
                key={layer}
                className={styles.layer}
                style={{ "--layer": layer } as React.CSSProperties}
              >
                <span className={styles.layerLines} />
                <span className={styles.layerLight} />
              </div>
            ))}
          </div>
          <div className={styles.assemblyRing} />
          <div className={styles.coordinates}>SYSTEM / REFINING</div>
        </>
      ) : (
        <>
          <div className={styles.signalRing} />
          <div className={styles.signalRing} />
          <div className={styles.signalRing} />
          <div className={styles.beacon}>
            <div className={styles.beaconHead}>
              <span />
              <span />
              <span />
            </div>
            <div className={styles.beaconBody}>
              <i />
              <i />
              <i />
              <span />
            </div>
            <div className={styles.beaconBase} />
          </div>
          <div className={styles.coordinates}>SIGNAL / SEARCHING</div>
          <span className={styles.signalDot} />
        </>
      )}
    </div>
  )
}

export function ServiceStatePage({
  state,
  onRecover,
}: {
  state: ServiceState
  onRecover?: () => void
}) {
  const router = useRouter()
  const [checking, setChecking] = useState(false)
  const [message, setMessage] = useState("")
  const copy = content[state]

  const checkConnection = useCallback(
    async (manual = false, signal?: AbortSignal) => {
      if (manual) setChecking(true)
      try {
        const response = await fetch("/api/service-status", {
          cache: "no-store",
          signal,
        })
        const result = (await response.json()) as { status: string }
        if (response.ok && result.status === "online") {
          if (onRecover) onRecover()
          else {
            router.push("/")
            router.refresh()
          }
        } else if (manual) {
          setMessage(
            result.status === "maintenance"
              ? "Maintenance is still in progress. Please check back shortly."
              : "Still no connection. We’ll keep checking automatically."
          )
        }
      } catch {
        if (manual)
          setMessage("Still no connection. We’ll keep checking automatically.")
      } finally {
        if (manual) setChecking(false)
      }
    },
    [onRecover, router]
  )

  useEffect(() => {
    if (state !== "offline") return
    const controller = new AbortController()
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible")
        void checkConnection(false, controller.signal)
    }, 15000)
    return () => {
      window.clearInterval(timer)
      controller.abort()
    }
  }, [state, checkConnection])

  return (
    <main className={styles.scene}>
      <header className={styles.header}>
        <Link href="/" className={styles.brand}>
          <LogoMark className="size-8" />
          <span>JustCD</span>
        </Link>
        <ThemePicker />
      </header>
      <section className={styles.content} aria-labelledby="service-state-title">
        <StateArtwork state={state} />
        <div className={styles.copy}>
          <p className={styles.eyebrow}>
            <span />
            {copy.code}
          </p>
          <h1 id="service-state-title">{copy.title}</h1>
          <p className={styles.description}>{copy.description}</p>
          <div className={styles.actions}>
            {state === "not-found" ? (
              <>
                <Button size="lg" render={<Link href="/" />}>
                  Back to overview <span aria-hidden="true">↗</span>
                </Button>
                <Button
                  size="lg"
                  variant="outline"
                  onClick={() =>
                    window.history.length > 1 ? router.back() : router.push("/")
                  }
                >
                  Go back
                </Button>
              </>
            ) : (
              <Button
                size="lg"
                onClick={() => void checkConnection(true)}
                loading={checking}
                loadingText="Checking connection…"
              >
                {state === "maintenance"
                  ? "Check availability"
                  : "Try reconnecting"}
                <span aria-hidden="true">↗</span>
              </Button>
            )}
          </div>
          <p role="status" className={styles.feedback}>
            {message ||
              (state === "offline"
                ? "Automatically checking every 15 seconds"
                : "")}
          </p>
        </div>
      </section>
      <footer className={styles.footer}>
        <span>GIT → PLAN → SYNC</span>
        <span>{copy.caption}</span>
      </footer>
    </main>
  )
}
