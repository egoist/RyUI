package sqlite

// Fetches is how many times each of d's live queries has fetched, by
// query.
func Fetches(d *DB) map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]int{}
	for w := range d.watches {
		w.mu.Lock()
		out[w.query] += w.fetches
		w.mu.Unlock()
	}
	return out
}

// Running reports whether any of d's live queries is fetching.
func Running(d *DB) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for w := range d.watches {
		w.mu.Lock()
		r := w.running
		w.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}
