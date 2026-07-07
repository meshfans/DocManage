package utils

import (
	"fmt"
	"unicode"
	"golang.org/x/crypto/bcrypt"
)

const (
	// 密码策略：8-18 位，必须包含数字、字母、符号中的至少 2 种
	MinPasswordLength = 8
	MaxPasswordLength = 18
)

func HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ValidatePasswordStrength 密码强度校验：
//   - 长度 8-18 位
//   - 至少包含 数字 / 字母 / 符号 中的 2 种
func ValidatePasswordStrength(password string) error {
	// 1. 长度校验
	length := len(password)
	if length < MinPasswordLength {
		return fmt.Errorf("密码长度至少%d位", MinPasswordLength)
	}
	if length > MaxPasswordLength {
		return fmt.Errorf("密码长度不能超过%d位", MaxPasswordLength)
	}

	// 2. 复杂度校验：统计字符类别
	hasDigit := false
	hasLetter := false
	hasSymbol := false
	for _, ch := range password {
		switch {
		case unicode.IsDigit(ch):
			hasDigit = true
		case unicode.IsLetter(ch):
			hasLetter = true
		case unicode.IsPunct(ch) || unicode.IsSymbol(ch):
			hasSymbol = true
		}
	}

	count := 0
	if hasDigit {
		count++
	}
	if hasLetter {
		count++
	}
	if hasSymbol {
		count++
	}
	if count < 2 {
		return fmt.Errorf("密码必须为%d-%d位，且至少包含数字、字母、符号中的两种", MinPasswordLength, MaxPasswordLength)
	}

	return nil
}
