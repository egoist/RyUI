# RyUI website

The home page and, at `/docs`, the guides of [`../docs`](../docs), built
with TanStack Start and shadcn/ui. Every page is prerendered at build
time, and Cloudflare Workers serves the result as static assets, with no
Worker code.

```sh
bun install                    # at the root of the repository
bun run --cwd website dev      # http://localhost:3000
bun run --cwd website build    # dist/client
bun run --cwd website preview  # dist/client, served by wrangler as in production
bun run --cwd website deploy   # build, then wrangler deploy
```

Pages of the docs are rendered from the markdown when the site is built:
the order of the sidebar comes from the lists of `docs/README.md`, and
links between pages, such as `shipping.md#ios`, become links of the site;
links to other files of the repository go to GitHub. Lists of pages,
`- [Title](page.md): what it covers`, become cards. Pages for maintainers
(`unpublished` in `src/lib/markdown-source.ts`) stay off the site.

The same build generates `/llms.txt`, an index of the docs as Markdown,
and `/llms-full.txt`, all pages combined; append `.md` to a page's URL
for its Markdown, `/docs/shipping.md`.

Blocks between `<!-- repository-only:start -->` and
`<!-- repository-only:end -->`, on lines of their own, show on GitHub
only.
