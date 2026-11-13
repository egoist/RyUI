import { createFileRoute } from "@tanstack/react-router"

import { loadSearchIndex } from "@/lib/docs.server"

// Prerendered to /zh/docs/search.json, which the search of the Chinese pages fetches.
export const Route = createFileRoute("/zh/docs/search.json")({
  server: {
    handlers: {
      GET: async () => Response.json(await loadSearchIndex("zh")),
    },
  },
})
