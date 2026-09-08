package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func publicTestJWK(t *testing.T, kid string) jose.JSONWebKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return jose.JSONWebKey{Key: &private.PublicKey, KeyID: kid, Algorithm: string(jose.ES256), Use: "sig"}
}

func TestStaticKeySetSelectsExactPublicKey(t *testing.T) {
	key := publicTestJWK(t, "current")
	set := NewStaticKeySet(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
	got, err := set.Key(context.Background(), "current", string(jose.ES256))
	if err != nil || got.KeyID != "current" || !got.IsPublic() {
		t.Fatalf("key = %+v, err = %v", got, err)
	}
	for _, tc := range []struct{ kid, algorithm string }{
		{"missing", string(jose.ES256)},
		{"current", string(jose.RS256)},
	} {
		if _, err := set.Key(context.Background(), tc.kid, tc.algorithm); err == nil {
			t.Fatalf("Key(%q, %q) unexpectedly succeeded", tc.kid, tc.algorithm)
		}
	}

	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cloned := NewStaticKeySet(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: private, KeyID: "private", Algorithm: string(jose.ES256), Use: "sig",
	}}})
	if got, err := cloned.Key(context.Background(), "private", string(jose.ES256)); err != nil || !got.IsPublic() {
		t.Fatalf("private key was not reduced to public material: %+v, %v", got, err)
	}
}

func TestRemoteKeySetCachesRefreshesAndUsesBoundedStaleKey(t *testing.T) {
	current := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{publicTestJWK(t, "current")}}
	var (
		mu     sync.RWMutex
		status = http.StatusOK
		calls  atomic.Int32
	)
	client := &http.Client{Transport: keyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		mu.RLock()
		defer mu.RUnlock()
		body := ""
		if status == http.StatusOK {
			encoded, _ := json.Marshal(current)
			body = string(encoded)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	remote, err := NewRemoteKeySet("https://auth.example/.well-known/jwks.json", client, time.Hour, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Key(context.Background(), "current", string(jose.ES256)); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Key(context.Background(), "current", string(jose.ES256)); err != nil || calls.Load() != 1 {
		t.Fatalf("cached lookup err=%v calls=%d", err, calls.Load())
	}

	mu.Lock()
	current = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{publicTestJWK(t, "next")}}
	mu.Unlock()
	if _, err := remote.Key(context.Background(), "next", string(jose.ES256)); err != nil || calls.Load() != 2 {
		t.Fatalf("rotated lookup err=%v calls=%d", err, calls.Load())
	}

	mu.Lock()
	status = http.StatusServiceUnavailable
	mu.Unlock()
	remote.fetchedAt = time.Now().Add(-remote.cacheTTL)
	if _, err := remote.Key(context.Background(), "next", string(jose.ES256)); err != nil {
		t.Fatalf("bounded stale key rejected: %v", err)
	}
	remote.fetchedAt = time.Now().Add(-remote.maxStale - time.Second)
	if _, err := remote.Key(context.Background(), "next", string(jose.ES256)); err == nil {
		t.Fatal("key older than max stale was accepted")
	}
}

func TestRemoteKeySetRejectsInvalidConfigurationAndDocuments(t *testing.T) {
	defaults, err := NewRemoteKeySet("https://auth.example/jwks", nil, time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.client.Timeout <= 0 {
		t.Fatal("default JWKS client has no request timeout")
	}
	if _, err := NewRemoteKeySet("relative", nil, time.Minute, time.Hour); err == nil {
		t.Fatal("relative JWKS URL accepted")
	}
	if _, err := NewRemoteKeySet("http://auth.example/jwks", nil, time.Minute, time.Hour); err == nil {
		t.Fatal("insecure remote JWKS URL accepted")
	}
	if _, err := NewRemoteKeySet("https://auth.example/jwks", nil, 0, time.Hour); err == nil {
		t.Fatal("zero cache TTL accepted")
	}

	for _, body := range []string{"not-json", `{"keys":[]}`} {
		t.Run(body, func(t *testing.T) {
			client := &http.Client{Transport: keyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			remote, err := NewRemoteKeySet("https://auth.example/.well-known/jwks.json", client, time.Minute, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := remote.Key(context.Background(), "missing", string(jose.ES256)); err == nil {
				t.Fatal("invalid JWKS accepted")
			}
		})
	}
}

type keyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f keyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
