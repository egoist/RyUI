package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// What the App Store Connect and Google Play clients share: signed JWTs,
// and requests retried as the stores ask (429, 5xx), with backoff.

// signJWT signs header and claims: ES256 (App Store Connect's keys) or
// RS256 (Google's service accounts).
func signJWT(header, claims map[string]any, key crypto.Signer) (string, error) {
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(h) + "." + enc.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	var sig []byte
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		// JWS's ES256 is r and s, each 32 bytes, not ASN.1's DER.
		r, s, err := ecdsa.Sign(rand.Reader, k, sum[:])
		if err != nil {
			return "", err
		}
		sig = make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
	case *rsa.PrivateKey:
		if sig, err = rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:]); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("a %T can't sign a JWT", key)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// storeHTTP is the stores' HTTP: retries, and the waits between them
// and between polls, which tests make instant.
type storeHTTP struct {
	client  *http.Client
	sleep   func(time.Duration)
	verbose bool
	// attempts is how many times a request is tried (default 6).
	attempts int
}

func newStoreHTTP(verbose bool) storeHTTP {
	// Redirects aren't followed: a resumable upload's 308 is "send more".
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return storeHTTP{client: client, sleep: time.Sleep, verbose: verbose, attempts: 6}
}

// errRetry wraps an error worth another try, after wait if not zero.
type errRetry struct {
	err  error
	wait time.Duration
}

func (e *errRetry) Error() string { return e.err.Error() }
func (e *errRetry) Unwrap() error { return e.err }

// retry runs f until it succeeds, fails for good, or runs out of
// attempts, waiting longer each time (1s, 2s, 4s... at most a minute, or
// what the server said).
func (s storeHTTP) retry(f func() error) error {
	attempts := max(s.attempts, 1)
	wait := time.Second
	for i := 1; ; i++ {
		err := f()
		var r *errRetry
		if err == nil || !errors.As(err, &r) {
			return err
		}
		if i == attempts {
			return r.err
		}
		d := wait
		if r.wait > 0 {
			d = r.wait
		}
		if s.verbose {
			fmt.Printf("asuka: %v; trying again in %s\n", r.err, d)
		}
		s.sleep(d)
		wait = min(wait*2, time.Minute)
	}
}

// retryable reports whether a response's status is worth another try:
// rate limited, or the server's trouble (for a request that's safe to
// repeat).
func retryable(status int, idempotent bool) bool {
	return status == http.StatusTooManyRequests || (idempotent && (status >= 500 || status == http.StatusRequestTimeout))
}

// retryAfter is a response's Retry-After, in seconds.
func retryAfter(h http.Header) time.Duration {
	if n, err := strconv.Atoi(h.Get("Retry-After")); err == nil && n > 0 {
		return min(time.Duration(n)*time.Second, 5*time.Minute)
	}
	return 0
}

// transient reports whether a request's error is the network's, so
// worth another try for a request that's safe to repeat.
func transient(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// mb formats a size in megabytes.
func mb(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) }
