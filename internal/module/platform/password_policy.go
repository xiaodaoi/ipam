package platform

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// 口令策略（M6-001）。所有设置口令的入口（创建账号、管理员重置、本人改密）
// 必须调用 ValidatePassword，避免各入口策略漂移。
const (
	PasswordMinLength = 10
	PasswordMaxLength = 128
)

// ErrWeakPassword 由调用方映射为 400 WEAK_PASSWORD。
var ErrWeakPassword = errors.New("WEAK_PASSWORD")

// ValidatePassword 校验口令强度并返回可直接展示给用户的错误。
// 规则：10–128 位；不含空白；至少 3 类字符（小写/大写/数字/符号）；
// 不得包含用户名（≥3 位时）；不得出现在常见弱口令表中。
func ValidatePassword(username, pw string) error {
	if len(pw) < PasswordMinLength {
		return fmt.Errorf("%w: 口令至少 %d 位", ErrWeakPassword, PasswordMinLength)
	}
	if len(pw) > PasswordMaxLength {
		return fmt.Errorf("%w: 口令最长 %d 位", ErrWeakPassword, PasswordMaxLength)
	}
	if strings.IndexFunc(pw, unicode.IsSpace) >= 0 {
		return fmt.Errorf("%w: 口令不能包含空白字符", ErrWeakPassword)
	}
	var lower, upper, digit, symbol bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			symbol = true
		}
	}
	classes := 0
	for _, ok := range []bool{lower, upper, digit, symbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return fmt.Errorf("%w: 口令需包含小写/大写/数字/符号中的至少 3 类", ErrWeakPassword)
	}
	if u := strings.TrimSpace(username); len(u) >= 3 && strings.Contains(strings.ToLower(pw), strings.ToLower(u)) {
		return fmt.Errorf("%w: 口令不能包含用户名", ErrWeakPassword)
	}
	switch strings.ToLower(pw) {
	case "admin123", "password", "password1", "12345678", "123456789", "qwerty123", "ipam123456":
		return fmt.Errorf("%w: 口令过于常见，请更换", ErrWeakPassword)
	}
	return nil
}
