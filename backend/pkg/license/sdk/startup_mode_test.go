package sdk

import (
	stdcrypto "crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"testing"

	"doc/pkg/license/crypto"
)

// TestResolveStartup_EmptyKey 验证 LicenseKey 为空时返回错误（2026-09-29 硬切）。
//
// 历史行为是「空 key 跳过校验、返回 nil outcome」，已下线：空 license 等同无效 license，
// 必须拒绝启动。此处测纯函数 resolveStartup（VerifyStartupOrLog 会 os.Exit，无法直接测）。
func TestResolveStartup_EmptyKey(t *testing.T) {
	out, err := resolveStartup(LicenseConfig{})
	if !errors.Is(err, ErrLicenseKeyMissing) {
		t.Errorf("empty license_key must be rejected, got err=%v", err)
	}
	if out != nil {
		t.Errorf("empty license_key should return nil outcome, got %+v", out)
	}
}

// TestResolveStartup_MissingPubKey 验证 LicenseKey 非空但找不到公钥时返回 error。
//
// 2026-09-29：此前该分支因 utils.Fatal 调 os.Exit 被 t.Skip 掉；抽出 resolveStartup
// 纯函数后，本分支首次获得真实覆盖。
func TestResolveStartup_MissingPubKey(t *testing.T) {
	fx := makeStartupFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", "")

	// 屏蔽 CLI flag 与 ./public.pem 兜底，确保四级来源全部失败。
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"doc-server"}

	if _, err := resolveStartup(LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	}); err == nil {
		t.Error("missing public key must be rejected")
	}
}

// TestResolveStartup_Expired 验证已过期的 license 返回 error（硬切后必须拒绝启动）。
func TestResolveStartup_Expired(t *testing.T) {
	fx := makeStartupFixture(t, -86400) // expires_at = now - 1 天
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	out, err := resolveStartup(LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	})
	if err == nil {
		t.Fatal("expired license must be rejected")
	}
	if out == nil || out.Status != StatusExpired {
		t.Errorf("expected StatusExpired outcome, got %+v", out)
	}
}

// TestResolveStartup_CorruptKey 验证被篡改 / 损坏的 license_key 返回 error。
func TestResolveStartup_CorruptKey(t *testing.T) {
	fx := makeStartupFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	cases := map[string]string{
		"empty-after-base64": base64.StdEncoding.EncodeToString([]byte("garbage-not-an-envelope")),
		"not-base64":         "!!!not-base64!!!",
		"random-valid-b64":   base64.StdEncoding.EncodeToString(make([]byte, 64)),
		"tampered-envelope":  tamperLastByte(fx.licenseKey),
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveStartup(LicenseConfig{
				AppID:       fx.appID,
				MachineCode: fx.machineCode,
				LicenseKey:  key,
			}); err == nil {
				t.Error("corrupted license_key must be rejected")
			}
		})
	}
}

// TestResolveStartup_MachineCodeMismatch 验证换机（machine_code 不一致）返回 error。
func TestResolveStartup_MachineCodeMismatch(t *testing.T) {
	fx := makeStartupFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	if _, err := resolveStartup(LicenseConfig{
		AppID:       fx.appID,
		MachineCode: "OTHER_MACHINE", // 与签发时不一致
		LicenseKey:  fx.licenseKey,
	}); err == nil {
		t.Error("machine_code mismatch must be rejected")
	}
}

// TestResolveStartup_ExpiringSoonStillPasses 验证临近过期（< 7 天但未过期）仍放行。
func TestResolveStartup_ExpiringSoonStillPasses(t *testing.T) {
	fx := makeStartupFixture(t, 3*86400) // 剩余 3 天 → EXPIRING_SOON
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	out, err := resolveStartup(LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	})
	if err != nil {
		t.Fatalf("expiring-soon license should still pass, got: %v", err)
	}
	if out == nil || out.Status != StatusExpiringSoon {
		t.Errorf("expected StatusExpiringSoon, got %+v", out)
	}
}

// tamperLastByte 翻转 base64 解码后最后一个字节，构造验签/解密失败的 envelope。
func tamperLastByte(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return b64
	}
	raw[len(raw)-1] ^= 0xFF
	return base64.StdEncoding.EncodeToString(raw)
}

// TestVerifyStartupOrLog_HappyPath 验证 LicenseKey 非空 + 公钥合法时走通。
func TestVerifyStartupOrLog_HappyPath(t *testing.T) {
	fx := makeStartupFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	out, err := VerifyStartupOrLog(LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	})
	if err != nil {
		t.Errorf("happy path should not error: %v", err)
	}
	if out == nil || out.Result == nil {
		t.Errorf("happy path should return outcome with payload, got %+v", out)
	}
}

// startupFixture 极简 fixture（仅 startup_mode_test.go 独立使用）。
type startupFixture struct {
	pubPEM      string
	appID       string
	machineCode string
	licenseKey  string
}

func makeStartupFixture(t *testing.T, expOffsetSec int64) startupFixture {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	pkix, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}))

	appID := "doc_crm_v1"
	machineCode := "MACHINE123"
	now := nowUnix()
	payload := LicensePayload{
		AppID:           appID,
		MachineCode:     machineCode,
		IssuedAt:        now,
		ExpiresAt:       now + expOffsetSec,
		PackageExpireAt: now + expOffsetSec,
		Algo:            "rsa2048-sha256+aes256gcm-hkdf",
	}
	payloadBytes, _ := json.Marshal(payload)
	hashed := sha256.Sum256(payloadBytes)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, stdcrypto.SHA256, hashed[:])
	plaintext := append([]byte{}, payloadBytes...)
	plaintext = append(plaintext, sig...)

	aesKey, err := crypto.DeriveAesKey(appID, machineCode, crypto.HKDFInfo)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	block, _ := aes.NewCipher(aesKey)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	ct := aead.Seal(nil, nonce, plaintext, []byte("lmp-license-v1"))
	env := append([]byte{}, nonce...)
	env = append(env, ct...)
	licenseKey := base64.StdEncoding.EncodeToString(env)

	return startupFixture{
		pubPEM:      pubPEM,
		appID:       appID,
		machineCode: machineCode,
		licenseKey:  licenseKey,
	}
}
