// Package webpush is the server's side of asuka/sdk/push on the web: it sends
// a Web Push message to a browser's subscription (push.Registration's
// Token on the web), as the app's service worker expects it, with only
// the standard library.
//
//   - Encrypt encrypts a payload for a subscription (RFC 8291: aes128gcm,
//     RFC 8188).
//   - Keys are the app server's VAPID key pair (RFC 8292): the public one
//     goes in asuka.json's web.vapidPublicKey, the browser subscribes with
//     it; Authorization signs the push service's token (ES256) with the
//     private one. GenerateKeys makes a pair.
//   - Sender.Send sends a Notification (the JSON the app's service worker
//     shows) or any payload to a subscription.
//
// Sending one:
//
//	keys := webpush.Keys{Public: os.Getenv("VAPID_PUBLIC"), Private: os.Getenv("VAPID_PRIVATE")}
//	s := webpush.Sender{Keys: keys, Subject: "mailto:ops@example.com"}
//	sub, err := webpush.ParseSubscription(token) // the app's push.Register's Token
//	err = s.Send(ctx, sub, webpush.Notification{Title: "Ada", Body: "Lunch?", Data: map[string]string{"url": "myapp://chat/7"}}, nil)
//	if errors.Is(err, webpush.ErrGone) {
//		// the subscription ended: forget it
//	}
//
// A server that uses another library (web-push for Node, pywebpush...)
// sends the same JSON: docs/web.md, Web Push.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Subscription is a browser's push subscription, as
// PushSubscription.toJSON() has it: what push.Register answers with on
// the web, as its Token.
type Subscription struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime,omitempty"`
	Keys           struct {
		// P256dh is the browser's public key (an uncompressed P-256
		// point), Auth its authentication secret (16 bytes), in base64url.
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// ParseSubscription parses a subscription's JSON (push.Registration's
// Token on the web).
func ParseSubscription(token string) (Subscription, error) {
	var s Subscription
	if err := json.Unmarshal([]byte(token), &s); err != nil {
		return s, fmt.Errorf("webpush: the subscription isn't JSON: %w", err)
	}
	if _, err := s.keys(); err != nil {
		return s, err
	}
	return s, nil
}

type subKeys struct {
	pub  *ecdh.PublicKey
	auth []byte
}

func (s Subscription) keys() (subKeys, error) {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return subKeys{}, fmt.Errorf("webpush: the subscription's endpoint %q isn't an http(s) URL", s.Endpoint)
	}
	p, err := decode(s.Keys.P256dh)
	if err != nil {
		return subKeys{}, fmt.Errorf("webpush: the subscription's p256dh: %w", err)
	}
	pub, err := ecdh.P256().NewPublicKey(p)
	if err != nil {
		return subKeys{}, fmt.Errorf("webpush: the subscription's p256dh isn't a P-256 public key: %w", err)
	}
	auth, err := decode(s.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return subKeys{}, errors.New("webpush: the subscription's auth isn't 16 bytes of base64url")
	}
	return subKeys{pub, auth}, nil
}

// decode decodes base64url, padded or not (or standard base64, as some
// browsers' JSON once had).
func decode(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

var b64 = base64.RawURLEncoding

// MaxPayload is the longest payload Encrypt takes: a push service takes
// 4096 bytes of body, less the header (86), the tag (16) and the padding
// delimiter.
const MaxPayload = 4096 - 86 - 16 - 1

// recordSize is the record size the header says (RFC 8188's rs): the
// body is one record.
const recordSize = 4096

// Encrypt encrypts payload for sub, as RFC 8291 says (aes128gcm), with a
// new key pair and salt: the request's body, sent with
// Content-Encoding: aes128gcm.
func Encrypt(sub Subscription, payload []byte) ([]byte, error) {
	k, err := sub.keys()
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return encrypt(k.pub, k.auth, payload, salt, as)
}

// encrypt is Encrypt with the salt and the server's key pair given (the
// tests' RFC 8291 vector).
func encrypt(ua *ecdh.PublicKey, auth, payload, salt []byte, as *ecdh.PrivateKey) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("webpush: a payload of %d bytes (at most %d)", len(payload), MaxPayload)
	}
	secret, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	uaPub, asPub := ua.Bytes(), as.PublicKey().Bytes()
	// RFC 8291 3.4: the input keying material, from the ECDH secret and
	// the authentication secret.
	prkKey, err := hkdf.Extract(sha256.New, secret, auth)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPub) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	// RFC 8188 2.2, 2.3: the content encryption key and the nonce.
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The header: salt, rs, idlen, keyid (the server's public key).
	out := make([]byte, 0, 16+4+1+len(asPub)+len(payload)+1+gcm.Overhead())
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	// The one record, ended by its padding delimiter (2: the last).
	plain := append(append(make([]byte, 0, len(payload)+1), payload...), 2)
	return gcm.Seal(out, nonce, plain, nil), nil
}

// Keys are the app server's VAPID key pair, in base64url (unpadded):
// Public an uncompressed P-256 point (65 bytes; asuka.json's
// web.vapidPublicKey), Private its scalar (32 bytes; keep it secret).
type Keys struct {
	Public  string `json:"publicKey"`
	Private string `json:"privateKey"`
}

// GenerateKeys makes a VAPID key pair. Make one for the app, once: the
// subscriptions are made with its public key, and a new one needs new
// subscriptions.
func GenerateKeys() (Keys, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	priv, err := k.Bytes()
	if err != nil {
		return Keys{}, err
	}
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return Keys{}, err
	}
	return Keys{Public: b64.EncodeToString(pub), Private: b64.EncodeToString(priv)}, nil
}

func (k Keys) private() (*ecdsa.PrivateKey, error) {
	b, err := decode(k.Private)
	if err != nil {
		return nil, fmt.Errorf("webpush: the VAPID private key: %w", err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), b)
	if err != nil {
		return nil, fmt.Errorf("webpush: the VAPID private key isn't a P-256 key: %w", err)
	}
	if k.Public != "" {
		pub, err := decode(k.Public)
		want, _ := priv.PublicKey.Bytes()
		if err != nil || !bytes.Equal(pub, want) {
			return nil, errors.New("webpush: the VAPID public key isn't the private key's")
		}
	}
	return priv, nil
}

// Authorization is the Authorization header's value for a push to
// endpoint (RFC 8292): "vapid t=<JWT>, k=<public key>", the JWT signed
// with ES256, for endpoint's origin, until exp (at most 24 hours from
// now), its sub subject (a mailto: or https: URL the push service can
// reach the server's operator at; Apple's requires one).
func (k Keys) Authorization(endpoint, subject string, exp time.Time) (string, error) {
	priv, err := k.private()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("webpush: the endpoint %q isn't a URL", endpoint)
	}
	claims := map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": exp.Unix()}
	if subject != "" {
		claims["sub"] = subject
	}
	c, _ := json.Marshal(claims)
	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	pub, _ := priv.PublicKey.Bytes()
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + b64.EncodeToString(pub), nil
}

// Notification is what the app's service worker shows, as its JSON (the
// payload docs/web.md describes): the fields of push.Message it gets.
type Notification struct {
	// ID is its tag: one with the same ID replaces it (default: a new one).
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
	// Data are the app's own keys (push.Message's Data); "url" is what a
	// tap opens the app with (ui.OnOpenURL).
	Data map[string]string `json:"data,omitempty"`
	// Category names the app's notify.SetCategories category: its actions.
	Category string `json:"category,omitempty"`
	Thread   string `json:"thread,omitempty"`
	Summary  string `json:"summary,omitempty"`
	// Image is a picture's URL (an https one, or an asset's path).
	Image string `json:"image,omitempty"`
	// Badge sets the installed app's badge.
	Badge  int  `json:"badge,omitempty"`
	Silent bool `json:"silent,omitempty"`
}

// Options are a push's headers (RFC 8030).
type Options struct {
	// TTL is how long the push service keeps it for a browser that's
	// offline (default 4 weeks; 0 with Now: only if it can deliver it now).
	TTL time.Duration
	Now bool
	// Urgency: "very-low", "low", "normal" or "high" (default: normal).
	Urgency string
	// Topic replaces a push with the same topic still waiting (32
	// base64url characters at most).
	Topic string
}

// ErrGone is the push service's answer for a subscription that ended (404
// or 410): forget it.
var ErrGone = errors.New("webpush: the subscription is gone")

// StatusError is a push service's refusal.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return "webpush: the push service answered " + strconv.Itoa(e.Status) + ": " + strings.TrimSpace(e.Body)
}

// Sender sends pushes with the app server's VAPID keys.
type Sender struct {
	Keys Keys
	// Subject is the JWT's sub: "mailto:you@example.com" or an https URL.
	Subject string
	// Client sends the requests (default http.DefaultClient).
	Client *http.Client
}

// Send sends n (a Notification, sent as its JSON; a []byte or string,
// sent as it is) to sub, encrypted, with o's headers (nil: the defaults).
// A subscription that ended is ErrGone.
func (s *Sender) Send(ctx context.Context, sub Subscription, n any, o *Options) error {
	var payload []byte
	switch v := n.(type) {
	case []byte:
		payload = v
	case string:
		payload = []byte(v)
	case nil:
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		payload = b
	}
	req, err := s.Request(ctx, sub, payload, o)
	if err != nil {
		return err
	}
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode >= 300:
		return &StatusError{Status: resp.StatusCode, Body: string(body)}
	}
	return nil
}

// Request is the push's request to sub's push service: payload encrypted
// (none: a push without data), the VAPID authorization, o's headers.
func (s *Sender) Request(ctx context.Context, sub Subscription, payload []byte, o *Options) (*http.Request, error) {
	if o == nil {
		o = &Options{}
	}
	if _, err := sub.keys(); err != nil {
		return nil, err
	}
	var body []byte
	if len(payload) > 0 {
		b, err := Encrypt(sub, payload)
		if err != nil {
			return nil, err
		}
		body = b
	}
	auth, err := s.Keys.Authorization(sub.Endpoint, s.Subject, time.Now().Add(12*time.Hour))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	ttl := o.TTL
	if ttl == 0 && !o.Now {
		ttl = 28 * 24 * time.Hour
	}
	req.Header.Set("TTL", strconv.FormatInt(int64(ttl/time.Second), 10))
	if len(body) > 0 {
		req.Header.Set("Content-Encoding", "aes128gcm")
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	if o.Urgency != "" {
		req.Header.Set("Urgency", o.Urgency)
	}
	if o.Topic != "" {
		req.Header.Set("Topic", o.Topic)
	}
	return req, nil
}
