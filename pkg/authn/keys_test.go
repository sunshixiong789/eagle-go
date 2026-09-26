package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

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
