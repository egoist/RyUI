These fixtures come from Taffy (https://github.com/DioxusLabs/taffy,
MIT, see LICENSE-taffy): each is an HTML test laid out by Chrome, with
Chrome's results as the expectations. Only the border-box, left-to-right
variants are kept. css_test.go runs them; known_failures.txt lists the
ones nui's layout doesn't match yet.
