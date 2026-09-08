package infrastructure

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

type TokenIssuer struct {
	signer   jose.Signer
	keys     jose.JSONWebKeySet
	issuer   string
	audience string
	ttl      time.Duration
}

// NewTokenIssuer loads a key ring from <kid>.pem files. The active key must
// contain private material; all other files may contain only public keys so a
// rolling key rotation can continue verifying tokens issued by the old key.
func NewTokenIssuer(keyDirectory, activeKeyID, issuer, audience string, ttl time.Duration) (*TokenIssuer, error) {
	root, err := os.OpenRoot(keyDirectory)
	if err != nil {
		return nil, fmt.Errorf("open token signing key directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("read token signing key directory: %w", err)
	}
	var (
		activeKey any
		activeAlg jose.SignatureAlgorithm
		public    []jose.JSONWebKey
	)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pem" {
			continue
		}
		kid := strings.TrimSuffix(entry.Name(), ".pem")
		data, err := root.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read token key %q: %w", kid, err)
		}
		key, err := parsePEMKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse token key %q: %w", kid, err)
		}
		algorithm, publicKey, private := signingKeyMetadata(key)
		if algorithm == "" {
			return nil, fmt.Errorf("token key %q must be P-256 EC or RSA with at least 2048 bits", kid)
		}
		public = append(public, jose.JSONWebKey{
			Key: publicKey, KeyID: kid, Algorithm: string(algorithm), Use: "sig",
		})
		if kid == activeKeyID {
			if !private {
				return nil, fmt.Errorf("active token key %q does not contain a private key", kid)
			}
			activeKey, activeAlg = key, algorithm
		}
	}
	if activeKey == nil {
		return nil, fmt.Errorf("active token key %q not found", activeKeyID)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: activeAlg, Key: activeKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", activeKeyID),
	)
	if err != nil {
		return nil, fmt.Errorf("create access token signer: %w", err)
	}
	return &TokenIssuer{
		signer: signer, keys: jose.JSONWebKeySet{Keys: public},
		issuer: issuer, audience: audience, ttl: ttl,
	}, nil
}

func (i *TokenIssuer) Issue(identity *domain.Identity, sessionID string, now time.Time) (string, error) {
	tokenID, err := randomTokenID()
	if err != nil {
		return "", err
	}
	claims := jwt.Claims{
		Issuer: i.issuer, Subject: identity.Subject, Audience: jwt.Audience{i.audience}, ID: tokenID,
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(i.ttl)),
	}
	private := map[string]any{"sid": sessionID, "roles": identity.Roles}
	token, err := jwt.Signed(i.signer).Claims(claims).Claims(private).Serialize()
	if err != nil {
		return "", fmt.Errorf("issue access token: %w", err)
	}
	return token, nil
}

func (i *TokenIssuer) PublicKeySet() jose.JSONWebKeySet {
	return jose.JSONWebKeySet{Keys: append([]jose.JSONWebKey(nil), i.keys.Keys...)}
}

func randomTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func parsePEMKey(data []byte) (any, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("PEM block not found")
	}
	switch block.Type {
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PUBLIC KEY":
		return x509.ParsePKIXPublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM block %q", block.Type)
	}
}

func signingKeyMetadata(key any) (jose.SignatureAlgorithm, any, bool) {
	switch key := key.(type) {
	case *ecdsa.PrivateKey:
		if key.Curve != elliptic.P256() {
			return "", nil, false
		}
		return jose.ES256, &key.PublicKey, true
	case *ecdsa.PublicKey:
		if key.Curve != elliptic.P256() {
			return "", nil, false
		}
		return jose.ES256, key, false
	case *rsa.PrivateKey:
		if key.N.BitLen() < 2048 {
			return "", nil, false
		}
		return jose.RS256, &key.PublicKey, true
	case *rsa.PublicKey:
		if key.N.BitLen() < 2048 {
			return "", nil, false
		}
		return jose.RS256, key, false
	default:
		return "", nil, false
	}
}
