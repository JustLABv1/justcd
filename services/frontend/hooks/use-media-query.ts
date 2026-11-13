"use client"

import { useCallback, useSyncExternalStore } from "react"

/** Whether a CSS media query matches, kept in sync with the viewport. `serverValue` is used before hydration. */
export function useMediaQuery(query: string, serverValue = true) {
  const subscribe = useCallback((listener: () => void) => {
    const list = window.matchMedia(query)
    list.addEventListener("change", listener)
    return () => list.removeEventListener("change", listener)
  }, [query])
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches, () => serverValue)
}
