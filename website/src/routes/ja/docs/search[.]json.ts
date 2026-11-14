import { createFileRoute } from "@tanstack/react-router"

import { loadSearchIndex } from "@/lib/docs.server"

// Prerendered to /ja/docs/search.json, which the search of the Japanese pages fetches.
export const Route = createFileRoute("/ja/docs/search.json")({
  server: {
    handlers: {
      GET: async () => Response.json(await loadSearchIndex("ja")),
    },
  },
})
