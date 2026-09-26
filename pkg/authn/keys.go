package authn

import (
	"context"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

var ErrSigningKeyNotFound = errors.New("authn: signing key not found")

// KeySource 根据令牌中的 kid 和签名算法查找验签公钥。
type KeySource interface {
	// Key 返回匹配 kid、兼容指定算法且可用于验签的唯一公钥；kid 未知时禁止退回任意其他密钥。
	// 密钥集合中无匹配或存在多个匹配时返回 ErrSigningKeyNotFound。
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
