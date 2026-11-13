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
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// RFC 8291, Appendix A: the example's keys, salt, secret and message.
const (
	rfcPlaintext = "When I grow up, I want to be a watermelon"
	rfcASPublic  = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcUAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcUAPrivate = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcAuth      = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcMessage   = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The RFC's example encrypts to the RFC's message.
func TestEncryptRFC8291(t *testing.T) {
	as, err := ecdh.P256().NewPrivateKey(mustDecode(t, rfcASPrivate))
	if err != nil {
		t.Fatal(err)
	}
	if got := b64.EncodeToString(as.PublicKey().Bytes()); got != rfcASPublic {
		t.Fatalf("as_public %s", got)
	}
	ua, err := ecdh.P256().NewPublicKey(mustDecode(t, rfcUAPublic))
	if err != nil {
		t.Fatal(err)
	}
	out, err := encrypt(ua, mustDecode(t, rfcAuth), []byte(rfcPlaintext), mustDecode(t, rfcSalt), as)
	if err != nil {
		t.Fatal(err)
	}
	if got := b64.EncodeToString(out); got != rfcMessage {
		t.Fatalf("got\n%s\nwant\n%s", got, rfcMessage)
	}
	// And the browser's side reads it back.
	uaPriv, _ := ecdh.P256().NewPrivateKey(mustDecode(t, rfcUAPrivate))
	plain, err := decrypt(out, uaPriv, mustDecode(t, rfcAuth))
	if err != nil || string(plain) != rfcPlaintext {
		t.Fatalf("decrypted %q, %v", plain, err)
	}
}

// decrypt is a browser's side of RFC 8291: the body, with the
// subscription's private key and secret.
func decrypt(body []byte, ua *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("short")
	}
	salt, idlen := body[:16], int(body[20])
	if binary.BigEndian.Uint32(body[16:20]) != recordSize || len(body) < 21+idlen {
		return nil, errors.New("bad header")
	}
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		return nil, err
	}
	secret, err := ua.ECDH(asPub)
	if err != nil {
		return nil, err
	}
	prkKey, _ := hkdf.Extract(sha256.New, secret, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPub.Bytes()), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexByte(plain, 2)
	if i < 0 || bytes.Count(plain[i+1:], []byte{0}) != len(plain)-i-1 {
		return nil, errors.New("no padding delimiter")
	}
	return plain[:i], nil
}

func subscription(t *testing.T, endpoint string) (Subscription, *ecdh.PrivateKey, []byte) {
	t.Helper()
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	tok, _ := json.Marshal(map[string]any{"endpoint": endpoint, "expirationTime": nil,
		"keys": map[string]string{"p256dh": b64.EncodeToString(ua.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)}})
	sub, err := ParseSubscription(string(tok))
	if err != nil {
		t.Fatal(err)
	}
	return sub, ua, auth
}

// Encrypt's output (a new key and salt each time) reads back, and a
// payload too long for a push is refused.
func TestEncryptRoundTrip(t *testing.T) {
	sub, ua, auth := subscription(t, "https://push.example.net/p/1")
	a, err := Encrypt(sub, []byte(`{"title":"Hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Encrypt(sub, []byte(`{"title":"Hi"}`))
	if bytes.Equal(a, b) {
		t.Fatal("the same body twice")
	}
	if p, err := decrypt(a, ua, auth); err != nil || string(p) != `{"title":"Hi"}` {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := Encrypt(sub, make([]byte, MaxPayload)); err != nil {
		t.Fatal(err)
	}
	if body, _ := Encrypt(sub, make([]byte, MaxPayload)); len(body) != 4096 {
		t.Fatalf("the longest payload's body is %d bytes", len(body))
	}
	if _, err := Encrypt(sub, make([]byte, MaxPayload+1)); err == nil {
		t.Fatal("a payload too long was encrypted")
	}
}

func TestParseSubscription(t *testing.T) {
	for _, bad := range []string{
		`nope`,
		`{"endpoint":"ftp://x/y","keys":{"p256dh":"` + rfcUAPublic + `","auth":"` + rfcAuth + `"}}`,
		`{"endpoint":"https://x/y","keys":{"p256dh":"AAAA","auth":"` + rfcAuth + `"}}`,
		`{"endpoint":"https://x/y","keys":{"p256dh":"` + rfcUAPublic + `","auth":"AAAA"}}`,
	} {
		if _, err := ParseSubscription(bad); err == nil {
			t.Errorf("%s: parsed", bad)
		}
	}
	// Padded standard base64 too.
	if _, err := ParseSubscription(`{"endpoint":"https://x/y","keys":{"p256dh":"` + rfcUAPublic + `","auth":"BTBZMqHH6r4Tts7J/aSIgg=="}}`); err != nil {
		t.Fatal(err)
	}
}

// verifyVAPID checks an Authorization header's JWT (ES256) against its
// key, and returns its claims.
func verifyVAPID(t *testing.T, header string) (claims map[string]any, key string) {
	t.Helper()
	rest, ok := strings.CutPrefix(header, "vapid t=")
	if !ok {
		t.Fatalf("header %q", header)
	}
	jwt, key, ok := strings.Cut(rest, ", k=")
	if !ok {
		t.Fatalf("header %q", header)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	h, _ := decode(parts[0])
	var head map[string]string
	if json.Unmarshal(h, &head) != nil || head["alg"] != "ES256" || head["typ"] != "JWT" {
		t.Fatalf("header %s", h)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustDecode(t, key))
	if err != nil {
		t.Fatal(err)
	}
	sig := mustDecode(t, parts[2])
	if len(sig) != 64 {
		t.Fatalf("a signature of %d bytes", len(sig))
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("the signature doesn't verify")
	}
	c, _ := decode(parts[1])
	if err := json.Unmarshal(c, &claims); err != nil {
		t.Fatal(err)
	}
	return claims, key
}

// RFC 8292's own example token verifies with its key (the verifier is
// right), and Authorization's tokens verify, for the endpoint's origin.
func TestVAPID(t *testing.T) {
	const rfcToken = "vapid t=eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJhdWQiOiJodHRwczovL3B1c2guZXhhbXBsZS5uZXQiLCJleHAiOjE0NTM1MjM3NjgsInN1YiI6Im1haWx0bzpwdXNoQGV4YW1wbGUuY29tIn0.i3CYb7t4xfxCDquptFOepC9GAu_HLGkMlMuCGSK2rpiUfnK9ojFwDXb1JrErtmysazNjjvW2L9OkSSHzvoD1oA, k=BA1Hxzyi1RUM1b5wjxsn7nGxAszw2u61m164i3MrAIxHF6YK5h4SDYic-dRuU_RCPCfA5aq9ojSwk5Y2EmClBPs"
	claims, _ := verifyVAPID(t, rfcToken)
	if claims["aud"] != "https://push.example.net" || claims["sub"] != "mailto:push@example.com" {
		t.Fatalf("the RFC's claims %v", claims)
	}

	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(mustDecode(t, keys.Public)) != 65 || len(mustDecode(t, keys.Private)) != 32 {
		t.Fatalf("keys %+v", keys)
	}
	exp := time.Unix(1_900_000_000, 0)
	h, err := keys.Authorization("https://fcm.googleapis.com/fcm/send/abc:def", "mailto:ops@example.com", exp)
	if err != nil {
		t.Fatal(err)
	}
	claims, k := verifyVAPID(t, h)
	if k != keys.Public || claims["aud"] != "https://fcm.googleapis.com" || claims["sub"] != "mailto:ops@example.com" || claims["exp"] != float64(exp.Unix()) {
		t.Fatalf("claims %v, key %s", claims, k)
	}
	// A public key that isn't the private key's is refused.
	other, _ := GenerateKeys()
	if _, err := (Keys{Public: other.Public, Private: keys.Private}).Authorization("https://x.example/1", "", exp); err == nil {
		t.Fatal("mismatched keys signed")
	}
}

// Send posts the encrypted payload with its headers to the endpoint,
// which reads it back; 410 is ErrGone.
func TestSend(t *testing.T) {
	keys, _ := GenerateKeys()
	var got struct {
		header http.Header
		body   []byte
	}
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	sub, ua, auth := subscription(t, srv.URL+"/push/7")
	s := &Sender{Keys: keys, Subject: "mailto:ops@example.com"}
	n := Notification{Title: "Ada", Body: "Lunch?", Data: map[string]string{"url": "myapp://chat/7"}, Category: "chat", Badge: 2}
	if err := s.Send(context.Background(), sub, n, &Options{Urgency: "high", Topic: "chat7"}); err != nil {
		t.Fatal(err)
	}
	if got.header.Get("Content-Encoding") != "aes128gcm" || got.header.Get("TTL") != "2419200" || got.header.Get("Urgency") != "high" || got.header.Get("Topic") != "chat7" {
		t.Fatalf("headers %v", got.header)
	}
	claims, _ := verifyVAPID(t, got.header.Get("Authorization"))
	if claims["aud"] != srv.URL {
		t.Fatalf("aud %v, want %s", claims["aud"], srv.URL)
	}
	plain, err := decrypt(got.body, ua, auth)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"title":"Ada","body":"Lunch?","data":{"url":"myapp://chat/7"},"category":"chat","badge":2}` {
		t.Fatalf("payload %s", plain)
	}
	status = http.StatusGone
	if err := s.Send(context.Background(), sub, "x", nil); !errors.Is(err, ErrGone) {
		t.Fatalf("410: %v", err)
	}
	status = http.StatusBadRequest
	var se *StatusError
	if err := s.Send(context.Background(), sub, nil, &Options{Now: true}); !errors.As(err, &se) || se.Status != 400 {
		t.Fatalf("400: %v", err)
	}
	if len(got.body) != 0 || got.header.Get("TTL") != "0" || got.header.Get("Content-Encoding") != "" {
		t.Fatalf("a push without data: %d bytes, %v", len(got.body), got.header)
	}
}
