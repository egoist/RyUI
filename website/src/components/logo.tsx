import type * as React from "react"

import { cn } from "@/lib/utils"

/**
 * ΛSUKΛ: the name in its geometric letters, each Λ an A without its bar;
 * lambda colors the two Λ apart from the rest.
 */
export function Wordmark({ className, lambda, ...props }: React.ComponentProps<"svg"> & { lambda?: string }) {
  return (
    <svg viewBox="0 0 710 100" role="img" aria-label="Asuka" className={cn("h-[0.9em] w-auto", className)} {...props}>
      <g fill={lambda ?? "currentColor"}>
        <path d="M0 100 49 0h20l49 100H96L59 24 22 100Z" />
        <path d="M592 100 641 0h20l49 100h-22l-37-76-37 76Z" />
      </g>
      <g fill="none" stroke="currentColor" strokeWidth="23">
        <path d="M266 10h-78a20 20 0 0 0 0 40h38a20 20 0 0 1 0 40h-78" />
        <path d="M306-5v60a35 35 0 0 0 35 35h28a35 35 0 0 0 35-35v-60" />
        <path d="M454-5v110M458 58l88-73M492 40l77 80" />
      </g>
    </svg>
  )
}

/** The icon: a red Λ in front of the orange octagons of a field. */
export function Mark({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <svg viewBox="0 0 1024 1024" aria-hidden className={cn("size-7", className)} {...props}>
      <rect width="1024" height="1024" rx="230" fill="#0e0d10" />
      <path d="M754 660 612 802H412L270 660V460l142-142h200l142 142Z" fill="none" stroke="#ff7a1a" strokeWidth="40" />
      <path d="M874 710 662 922H362L150 710V410l212-212h300l212 212Z" fill="none" stroke="#ff7a1a" strokeWidth="30" opacity=".5" />
      <path d="M192 860 492 190h40l300 670H679L512 488 345 860Z" fill="#e3212b" stroke="#0e0d10" strokeWidth="56" strokeLinejoin="round" paintOrder="stroke" />
    </svg>
  )
}
