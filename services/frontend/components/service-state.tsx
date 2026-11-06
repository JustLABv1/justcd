"use client"

import Link from "next/link"
import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { LogoMark } from "@/components/logo"
import { ThemePicker } from "@/components/theme-picker"
import styles from "./service-state.module.css"

export type ServiceState = "maintenance" | "not-found" | "offline" | "pending-approval"

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
  "pending-approval": {
    code: "Access pending",
    title: "Awaiting approval.",
    description:
      "An instance administrator needs to approve your account. This page continues on its own as soon as they do.",
    caption: "Reviewing your access",
  },
  offline: {
    code: "Connection interrupted",
    title: "Waiting for a signal.",
    description:
      "We can’t reach the JustCD backend right now. We’ll keep checking and reconnect when it’s available.",
    caption: "Listening for the delivery engine",
  },
}

function initials(name: string) {
  const parts = name.replace(/@.*$/, "").split(/[\s._-]+/).filter(Boolean)
  return (parts.length > 1 ? parts[0][0] + parts[1][0] : (parts[0] ?? "?").slice(0, 2)).toUpperCase()
}

function StateArtwork({ state, account }: { state: ServiceState; account?: { name: string; email: string } }) {
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
      ) : state === "pending-approval" ? (
        <>
          <div className={styles.passRing} />
          <div className={`${styles.passRing} ${styles.passRingTwo}`} />
          <div className={styles.pass}>
            <div className={styles.passTop}>
              <span className={styles.passAvatar}>{initials(account?.name ?? "")}</span>
              <span className={styles.passName}>
                <i />
                <i />
              </span>
              <span className={styles.passStatus} />
            </div>
            <div className={styles.passLines}>
              <i />
              <i />
              <i />
            </div>
            <span className={styles.passScan} />
          </div>
          <ol className={styles.rail}>
            <li className={styles.railDone}>
              <span>✓</span>
              Requested
            </li>
            <li className={styles.railActive}>
              <span />
              In review
            </li>
            <li>
              <span />
              Access
            </li>
          </ol>
          <div className={styles.coordinates}>ACCESS / IN REVIEW</div>
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
  account,
  recheck,
  onSignOut,
}: {
  state: ServiceState
  onRecover?: () => void
  /** Pending approval only: who is waiting. */
  account?: { name: string; email: string }
  /** Pending approval only: resolves true once access was granted (the caller then moves on). */
  recheck?: () => Promise<boolean>
  onSignOut?: () => void
}) {
  const router = useRouter()
  const [checking, setChecking] = useState(false)
  const [message, setMessage] = useState("")
  const copy = content[state]

  const checkConnection = useCallback(
    async (manual = false, signal?: AbortSignal) => {
      if (manual) setChecking(true)
      if (recheck) {
        try {
          const granted = await recheck()
          if (!granted && manual) setMessage("Not approved yet. We’ll keep checking automatically.")
        } finally {
          if (manual) setChecking(false)
        }
        return
      }
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
    [onRecover, recheck, router]
  )

  useEffect(() => {
    if (state === "not-found" || (onRecover && !recheck)) return
    const controller = new AbortController()
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible")
        void checkConnection(false, controller.signal)
    }, 15000)
    return () => {
      window.clearInterval(timer)
      controller.abort()
    }
  }, [state, checkConnection, onRecover, recheck])

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
        <StateArtwork state={state} account={account} />
        <div className={styles.copy}>
          <p className={styles.eyebrow}>
            <span />
            {copy.code}
          </p>
          <h1 id="service-state-title">{copy.title}</h1>
          <p className={styles.description}>{copy.description}</p>
          {state === "pending-approval" && account && (
            <p className={styles.account}>
              Signed in as <strong>{account.email}</strong>
            </p>
          )}
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
            ) : state === "pending-approval" ? (
              <>
                <Button size="lg" onClick={() => void checkConnection(true)} loading={checking} loadingText="Checking…">
                  Check again <span aria-hidden="true">↗</span>
                </Button>
                <Button size="lg" variant="outline" onClick={onSignOut}>
                  Sign out
                </Button>
                <Button size="lg" variant="ghost" nativeButton={false} render={<Link href="/settings/security" />}>
                  Account security
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
          {state === "maintenance" && (
            <Button
              variant="link"
              nativeButton={false}
              render={<Link href="/login" />}
              className="mt-3"
            >
              Administrator sign in
            </Button>
          )}
          <p role="status" className={styles.feedback}>
            {message ||
              (state === "offline" || state === "pending-approval"
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
