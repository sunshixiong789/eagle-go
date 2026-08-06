package password

import (
	"errors"
	"strings"
	"testing"
)

// 测试里把代价参数压到最低，否则每个用例几十毫秒，整体太慢。
func testParams() Params {
	return Params{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

func TestHashAndVerify(t *testing.T) {
	const plain = "correct horse battery staple"

	encoded, err := HashWithParams(plain, testParams())
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	if err := Verify(plain, encoded); err != nil {
		t.Errorf("正确口令应校验通过, got %v", err)
	}
	if err := Verify("wrong password", encoded); !errors.Is(err, ErrMismatch) {
		t.Errorf("错误口令应返回 ErrMismatch, got %v", err)
	}
}

// 同一口令两次哈希必须不同——盐是随机的。
// 若相同则说明盐没有生效，彩虹表将直接可用。
func TestHashIsSalted(t *testing.T) {
	const plain = "same-password"

	first, err := HashWithParams(plain, testParams())
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	second, err := HashWithParams(plain, testParams())
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	if first == second {
		t.Error("相同口令的两次哈希不应相同（盐未生效）")
	}
	// 但两者都必须能校验通过
	if err := Verify(plain, first); err != nil {
		t.Errorf("first 校验失败: %v", err)
	}
	if err := Verify(plain, second); err != nil {
		t.Errorf("second 校验失败: %v", err)
	}
}

func TestHashFormatIsPHC(t *testing.T) {
	encoded, err := HashWithParams("x", testParams())
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Fatalf("PHC 串应有 6 段, got %d: %q", len(parts), encoded)
	}
	if parts[1] != "argon2id" {
		t.Errorf("算法标识 = %q, want argon2id", parts[1])
	}
	if parts[2] != "v=19" {
		t.Errorf("版本 = %q, want v=19", parts[2])
	}
	if !strings.HasPrefix(parts[3], "m=") {
		t.Errorf("参数段格式不对: %q", parts[3])
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{"空串", ""},
		{"段数不足", "$argon2id$v=19$m=8192,t=1,p=1$onlysalt"},
		{"算法不支持", "$bcrypt$v=19$m=8192,t=1,p=1$c2FsdA$aGFzaA"},
		{"版本不支持", "$argon2id$v=16$m=8192,t=1,p=1$c2FsdA$aGFzaA"},
		{"参数段损坏", "$argon2id$v=19$garbage$c2FsdA$aGFzaA"},
		{"盐不是合法 base64", "$argon2id$v=19$m=8192,t=1,p=1$!!!!$aGFzaA"},
		{"纯文本", "plaintext-password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify("anything", tt.encoded)
			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("应返回 ErrInvalidHash, got %v", err)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	weak := Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	strong := Params{Memory: 64 * 1024, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32}

	weakHash, err := HashWithParams("pw", weak)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	strongHash, err := HashWithParams("pw", strong)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	if !NeedsRehash(weakHash, strong) {
		t.Error("弱参数生成的哈希应被判定为需要升级")
	}
	if NeedsRehash(strongHash, strong) {
		t.Error("已达标的哈希不应被判定为需要升级")
	}
	// 解析不了的一律视为需要重算，避免脏数据一直留在库里
	if !NeedsRehash("not-a-hash", strong) {
		t.Error("非法哈希串应被判定为需要升级")
	}
}

// 存量哈希用旧参数生成，调参后仍必须能校验通过——
// 参数是随串存储的，这正是选 PHC 格式的原因。
func TestVerifyWorksAcrossParamChanges(t *testing.T) {
	oldParams := Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

	encoded, err := HashWithParams("legacy-password", oldParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	// 即便当前默认参数已提高，旧串依然要能验通过
	if err := Verify("legacy-password", encoded); err != nil {
		t.Errorf("旧参数生成的哈希应仍可校验, got %v", err)
	}
}
