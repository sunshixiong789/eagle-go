package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

var ErrSigningKeyNotFound = errors.New("authn: signing key not found")

// KeySource resolves a public verification key by both kid and algorithm.
// Implementations must never fall back to an arbitrary key when kid is unknown.
type KeySource interface {
	Key(context.Context, string, string) (jose.JSONWebKey, error)
}

type StaticKeySet struct {
	set jose.JSONWebKeySet
}

func NewStaticKeySet(set jose.JSONWebKeySet) *StaticKeySet {
	return &StaticKeySet{set: publicClone(set)}
}

func (s *StaticKeySet) Key(_ context.Context, kid, algorithm string) (jose.JSONWebKey, error) {
	return selectKey(s.set, kid, algorithm)
}

// RemoteKeySet caches an authentication center's JWKS. An unknown kid forces
// one refresh, while a temporarily unavailable endpoint may use a bounded stale
// key so an auth-center outage does not immediately take every resource service down.
type RemoteKeySet struct {
	url      string
	client   *http.Client
	cacheTTL time.Duration
	maxStale time.Duration

	mu        sync.Mutex
	set       jose.JSONWebKeySet
	fetchedAt time.Time
}

func NewRemoteKeySet(rawURL string, client *http.Client, cacheTTL, maxStale time.Duration) (*RemoteKeySet, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("authn: JWKS URL must be absolute")
	}
	host := u.Hostname()
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !loopback {
		return nil, errors.New("authn: JWKS URL must use HTTPS outside loopback development")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if cacheTTL <= 0 || maxStale < cacheTTL {
		return nil, errors.New("authn: JWKS cache TTL must be positive and max stale must be at least the cache TTL")
	}
	return &RemoteKeySet{url: rawURL, client: client, cacheTTL: cacheTTL, maxStale: maxStale}, nil
}

func (r *RemoteKeySet) Key(ctx context.Context, kid, algorithm string) (jose.JSONWebKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.fetchedAt) < r.cacheTTL {
		if key, err := selectKey(r.set, kid, algorithm); err == nil {
			return key, nil
		}
	}
	previous, previousAt := r.set, r.fetchedAt
	if err := r.refresh(ctx); err != nil {
		if !previousAt.IsZero() && now.Sub(previousAt) <= r.maxStale {
			if key, keyErr := selectKey(previous, kid, algorithm); keyErr == nil {
				return key, nil
			}
		}
		return jose.JSONWebKey{}, err
	}
	return selectKey(r.set, kid, algorithm)
}

func (r *RemoteKeySet) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return fmt.Errorf("authn: create JWKS request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("authn: fetch JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authn: fetch JWKS: unexpected HTTP status %d", resp.StatusCode)
	}
	var set jose.JSONWebKeySet
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&set); err != nil {
		return fmt.Errorf("authn: decode JWKS: %w", err)
	}
	set = publicClone(set)
	if len(set.Keys) == 0 {
		return errors.New("authn: JWKS contains no usable public signing keys")
	}
	r.set = set
	r.fetchedAt = time.Now()
	return nil
}

func selectKey(set jose.JSONWebKeySet, kid, algorithm string) (jose.JSONWebKey, error) {
	var matched *jose.JSONWebKey
	for _, key := range set.Key(kid) {
		if !key.Valid() || !key.IsPublic() || key.Use != "" && key.Use != "sig" || key.Algorithm != "" && key.Algorithm != algorithm {
			continue
		}
		if matched != nil {
			return jose.JSONWebKey{}, fmt.Errorf("%w: duplicate kid %q", ErrSigningKeyNotFound, kid)
		}
		copy := key
		matched = &copy
	}
	if matched == nil {
		return jose.JSONWebKey{}, fmt.Errorf("%w: kid %q algorithm %q", ErrSigningKeyNotFound, kid, algorithm)
	}
	return *matched, nil
}

func publicClone(set jose.JSONWebKeySet) jose.JSONWebKeySet {
	out := jose.JSONWebKeySet{Keys: make([]jose.JSONWebKey, 0, len(set.Keys))}
	for _, key := range set.Keys {
		if !key.Valid() {
			continue
		}
		if !key.IsPublic() {
			key = key.Public()
		}
		out.Keys = append(out.Keys, key)
	}
	return out
}
