package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// 恢复码（M6-007A）：10 位、32 进制字母表（去掉易混淆的 I/O/0/1），约 50 bit 熵；
// 仅以 SHA-256 落库，使用一次后立即从列表移除。
const (
	recoveryCodeCount    = 10
	recoveryCodeLen      = 10
	recoveryCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var ErrRecoveryCodeExhausted = errors.New("恢复码已用尽")

// GenerateRecoveryCodes 生成 n 个明文恢复码及对应散列（明文仅返回一次）。
func GenerateRecoveryCodes(n int) ([]string, []string, error) {
	if n <= 0 {
		n = recoveryCodeCount
	}
	codes := make([]string, 0, n)
	hashes := make([]string, 0, n)
	buf := make([]byte, recoveryCodeLen)
	for i := 0; i < n; i++ {
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, err
		}
		raw := make([]byte, recoveryCodeLen)
		for j, b := range buf {
			raw[j] = recoveryCodeAlphabet[int(b)%len(recoveryCodeAlphabet)]
		}
		code := string(raw[:5]) + "-" + string(raw[5:])
		codes = append(codes, code)
		hashes = append(hashes, RecoveryCodeHash(code))
	}
	return codes, hashes, nil
}

// NormalizeRecoveryCode 去分隔符/空白并转大写，便于用户按任意大小写输入。
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RecoveryCodeHash 恢复码散列（归一化后 SHA-256）。
func RecoveryCodeHash(code string) string {
	sum := sha256.Sum256([]byte(NormalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

// ConsumeRecoveryCode 校验并消耗一个恢复码；成功返回移除该码后的列表。
func ConsumeRecoveryCode(hashes []string, input string) ([]string, bool) {
	norm := NormalizeRecoveryCode(input)
	// 恢复码固定 10 位；不符合形状直接失败，避免无意义比较。
	if len(norm) != recoveryCodeLen {
		return hashes, false
	}
	h := RecoveryCodeHash(norm)
	for i, stored := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(stored)) == 1 {
			out := append([]string{}, hashes[:i]...)
			out = append(out, hashes[i+1:]...)
			return out, true
		}
	}
	return hashes, false
}
