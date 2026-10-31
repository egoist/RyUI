import type * as React from "react"

import { cn } from "@/lib/utils"

/** The name, set as the site's wordmark. */
export function Wordmark({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span className={cn("font-semibold tracking-tight", className)} {...props}>
      RyUI
    </span>
  )
}
