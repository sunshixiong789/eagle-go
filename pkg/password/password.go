// Package password 实现 argon2id 口令哈希。
//
// 选 argon2id 而不是 bcrypt：argon2id 是 OWASP 当前的首选，
// 内存硬（memory-hard）特性让 GPU/ASIC 批量爆破的成本远高于 bcrypt，
// 且不像 bcrypt 那样有 72 字节的输入截断问题。
//
// 哈希串采用 PHC 标准格式，参数随串存储，日后调参不影响存量口令的验证：
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt-base64>$<hash-base64>
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	// ErrMismatch 表示口令与哈希不匹配。
	ErrMismatch = errors.New("password: mismatch")
	// ErrInvalidHash 表示哈希串格式不合法或使用了不支持的算法版本。
	ErrInvalidHash = errors.New("password: invalid hash format")
)

// Params 是 argon2id 的代价参数。
type Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams 对应 OWASP 推荐的 argon2id 配置（64 MiB / 3 轮）。
// 单次哈希约几十毫秒，登录接口可以接受；如果你的机器更强，
// 优先加 Memory 而不是 Iterations。
func DefaultParams() Params {
	p := uint8(runtime.NumCPU())
	if p > 4 {
		p = 4
	}
	if p < 1 {
		p = 1
	}
	return Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: p,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// Hash 用 DefaultParams 生成 PHC 格式的哈希串。
func Hash(plain string) (string, error) {
	return HashWithParams(plain, DefaultParams())
}

// HashWithParams 允许指定代价参数，主要供测试压低成本使用。
func HashWithParams(plain string, p Params) (string, error) {
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: read salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify 校验明文口令是否匹配 encoded 哈希串。
// 不匹配返回 ErrMismatch，哈希串本身有问题返回 ErrInvalidHash。
//
// 调用方对外不要区分这两种错误，也不要区分"用户不存在"——
// 一律回同一句话，否则会形成账号枚举侧信道。
func Verify(plain, encoded string) error {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return err
	}

	got := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	// 定长比较，避免按字节短路造成的计时侧信道
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// NeedsRehash 判断存量哈希是否是用比当前配置更弱的参数生成的。
// 登录成功后可据此透明升级：verify 通过 -> NeedsRehash -> 重新 Hash 并落库。
func NeedsRehash(encoded string, want Params) bool {
	p, _, _, err := decode(encoded)
	if err != nil {
		// 解析不了的一律视为需要重算
		return true
	}
	return p.Memory < want.Memory ||
		p.Iterations < want.Iterations ||
		p.KeyLength < want.KeyLength
}

func decode(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return p, nil, nil, ErrInvalidHash
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return p, nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
