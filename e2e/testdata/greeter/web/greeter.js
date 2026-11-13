// The greeter package's JavaScript: its module and its view, on the web.

export function makeGreeter() {
  return { hello: (name) => `hello ${name} from the web` };
}

// A label showing its name; it sends shown with what it shows.
export function makeBadge(events) {
  const el = document.createElement("span");
  return {
    el,
    setName(name) {
      el.textContent = `web badge for ${name}`;
      events.shown(el.textContent);
    },
  };
}
