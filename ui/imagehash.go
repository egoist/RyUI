package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/internal/imagehash"
)

// Hash placeholders (ImageProps' BlurHash and ThumbHash): decoded once
// per hash and kept, as the PlaceholderImage prop's bytes, in two
// generations of up to 256 each: a list scrolling back and forth over
// its rows decodes each hash once.

type hashCache struct {
	cur, old map[string][]byte
}

const hashCacheSize = 256

var hashImages hashCache

func (c *hashCache) get(key string) ([]byte, bool) {
	if v, ok := c.cur[key]; ok {
		return v, true
	}
	if v, ok := c.old[key]; ok {
		c.put(key, v)
		return v, true
	}
	return nil, false
}

func (c *hashCache) put(key string, v []byte) {
	if c.cur == nil || len(c.cur) >= hashCacheSize {
		c.old, c.cur = c.cur, make(map[string][]byte, hashCacheSize)
	}
	c.cur[key] = v
}

// placeholderBytes is the PlaceholderImage prop of w×h RGBA pixels.
func placeholderBytes(w, h int, rgba []byte) []byte {
	b := make([]byte, 8, 8+len(rgba))
	binary.LittleEndian.PutUint32(b, uint32(w))
	binary.LittleEndian.PutUint32(b[4:], uint32(h))
	return append(b, rgba...)
}

// blurHashImage is a BlurHash's picture at ratio (width over height; 0:
// square), 32 pixels on its longer side: a blur needs no more. nil (no
// placeholder) for a malformed hash.
func blurHashImage(hash string, ratio float32) []byte {
	if hash == "" {
		return nil
	}
	w, h := 32, 32
	if ratio > 1 {
		h = max(1, int(math.Round(32/float64(ratio))))
	} else if ratio > 0 {
		w = max(1, int(math.Round(32*float64(ratio))))
	}
	key := string(rune('0'+w)) + string(rune('0'+h)) + hash
	if v, ok := hashImages.get(key); ok {
		return v
	}
	px, err := imagehash.BlurHash(hash, w, h, 1)
	var v []byte
	if err == nil {
		v = placeholderBytes(w, h, px)
	}
	hashImages.put(key, v)
	return v
}

// thumbHashImage is a ThumbHash's picture (base64); nil for a malformed
// hash.
func thumbHashImage(hash string) []byte {
	if hash == "" {
		return nil
	}
	key := "t" + hash
	if v, ok := hashImages.get(key); ok {
		return v
	}
	var v []byte
	if b, err := imagehash.ParseThumbHash(hash); err == nil {
		if w, h, px, err := imagehash.ThumbHash(b); err == nil {
			v = placeholderBytes(w, h, px)
		}
	}
	hashImages.put(key, v)
	return v
}
