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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doc/pkg/license/crypto"
)

// ============== 测试 fixture：生成一对密钥 + 一个合法 license_key ==============

type testFixture struct {
	priv        *rsa.PrivateKey
	pubPEM      string
	appID       string
	machineCode string
	payload     LicensePayload
	licenseKey  string // base64 envelope
}

func newFixture(t *testing.T, expOffsetSec int64) testFixture {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}

	pkixBytes, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkixBytes}))

	appID := "doc_crm_v1"
	machineCode := "AB33FF5BD2AEACE2D4535129ADC25DC5"

	now := nowUnix()
	payload := LicensePayload{
		AppID:           appID,
		MachineCode:     machineCode,
		Password:        "secret-pwd",
		Features:        []string{"crm", "audit"},
		IssuedAt:        now,
		ExpiresAt:       now + expOffsetSec,
		PackageExpireAt: now + expOffsetSec,
		CompanySize:     "medium",
		Algo:            "rsa2048-sha256+aes256gcm-hkdf",
	}

	// 1) 序列化 payload（**保留字节序**——这是签名的输入）
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	// 2) 签名
	hashed := sha256.Sum256(payloadBytes)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, stdcrypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// 3) 拼接 plaintext = payload || sig
	plaintext := append([]byte{}, payloadBytes...)
	plaintext = append(plaintext, sig...)

	// 4) HKDF 派生 AES key
	aesKey, err := crypto.DeriveAesKey(appID, machineCode, crypto.HKDFInfo)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	// 5) AES-GCM Seal
	block, _ := aes.NewCipher(aesKey)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	ct := aead.Seal(nil, nonce, plaintext, []byte("lmp-license-v1"))

	env := append([]byte{}, nonce...)
	env = append(env, ct...)
	licenseKey := base64.StdEncoding.EncodeToString(env)

	return testFixture{
		priv:        priv,
		pubPEM:      pubPEM,
		appID:       appID,
		machineCode: machineCode,
		payload:     payload,
		licenseKey:  licenseKey,
	}
}



// ============== ClassifyTimeStatus ==============

func TestClassifyTimeStatus(t *testing.T) {
	now := int64(1_000_000)
	cases := []struct {
		name      string
		expiresAt int64
		want      Status
	}{
		{"far future", now + 30*86400, StatusValid},
		{"exactly 7 days", now + 7*86400, StatusValid},
		{"6 days", now + 6*86400, StatusExpiringSoon},
		{"1 second", now + 1, StatusExpiringSoon},
		{"equal now", now, StatusExpiringSoon}, // 边界：now <= expiresAt
		{"past", now - 1, StatusExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyTimeStatus(now, c.expiresAt)
			if got != c.want {
				t.Errorf("got=%s want=%s", got, c.want)
			}
		})
	}
}

// ============== LicenseConfig.Validate ==============

func TestLicenseConfig_Validate(t *testing.T) {
	base := LicenseConfig{AppID: "a", MachineCode: "m", LicenseKey: "k"}
	if err := base.Validate(); err != nil {
		t.Errorf("base should be valid: %v", err)
	}
	if err := (LicenseConfig{}).Validate(); err == nil {
		t.Error("empty config should fail")
	}
	if err := (LicenseConfig{AppID: "a"}).Validate(); err == nil {
		t.Error("missing machineCode should fail")
	}
	if err := (LicenseConfig{AppID: "a", MachineCode: "m"}).Validate(); err == nil {
		t.Error("missing licenseKey should fail")
	}
}

// ============== ResolvePublicKey ==============

func TestResolvePublicKey_P1Injected(t *testing.T) {
	fx := newFixture(t, 30*86400)
	pem, src, err := ResolvePublicKey(fx.pubPEM, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasPrefix(src, "external injection") {
		t.Errorf("source=%q want 'external injection'", src)
	}
	if !strings.Contains(pem, "BEGIN") {
		t.Error("pem should contain BEGIN")
	}
}

func TestResolvePublicKey_P1EnvVar(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	pem, src, err := ResolvePublicKey("", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src != "env:LICENSE_PUBKEY_PEM" {
		t.Errorf("source=%q", src)
	}
	if !strings.Contains(pem, "BEGIN") {
		t.Error("pem missing")
	}
}

func TestResolvePublicKey_P2CliFlag(t *testing.T) {
	fx := newFixture(t, 30*86400)

	// 清空 env + 不存在 pemFile，避免 P1/P3 命中；只让 P2 CLI 命中
	t.Setenv("LICENSE_PUBKEY_PEM", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	// 测试三种 CLI flag 都能被识别（registerd 当前用 --pub-key）
	for _, flag := range []string{"--pub-key", "--pubkey", "--pubkey-pem"} {
		t.Run(flag, func(t *testing.T) {
			os.Args = []string{"doc-server", flag, fx.pubPEM}
			pem, src, err := ResolvePublicKey("", "")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if src != "cli:"+flag {
				t.Errorf("source=%q want 'cli:%s'", src, flag)
			}
			if !strings.Contains(pem, "BEGIN") {
				t.Error("pem missing")
			}
		})
	}
}

func TestResolvePublicKey_P2CliFlagBadPEM(t *testing.T) {
	t.Setenv("LICENSE_PUBKEY_PEM", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	// 命中 CLI 但内容不是合法 PEM（无 BEGIN marker）→ 应报错
	os.Args = []string{"doc-server", "--pub-key", "not-a-pem"}
	if _, _, err := ResolvePublicKey("", ""); err == nil {
		t.Error("expected error when CLI PEM is malformed")
	}
}

func TestResolvePublicKey_P3File(t *testing.T) {
	fx := newFixture(t, 30*86400)

	// 把公钥写到临时文件 + 通过 pemFile 参数显式指定路径
	dir := t.TempDir()
	path := filepath.Join(dir, "pub.pem")
	if err := os.WriteFile(path, []byte(fx.pubPEM), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("LICENSE_PUBKEY_PEM", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"doc-server"} // 无 CLI flag

	pem, src, err := ResolvePublicKey("", path)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src != "file: "+path {
		t.Errorf("source=%q want 'file: %s'", src, path)
	}
	if !strings.Contains(pem, "BEGIN") {
		t.Error("pem missing")
	}
}

func TestResolvePublicKey_AllFail(t *testing.T) {
	// 清空 env + 指定不存在的文件路径 + 无 CLI flag，确保触发最终错误
	t.Setenv("LICENSE_PUBKEY_PEM", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"doc-server"}

	if _, _, err := ResolvePublicKey("", filepath.Join(t.TempDir(), "nonexistent.pem")); err == nil {
		t.Error("expected error when all sources fail")
	}
}

// ============== VerifyLicense 完整流程 ==============

func TestVerifyLicense_HappyPath(t *testing.T) {
	fx := newFixture(t, 30*86400) // 30 天后过期
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	cfg := LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	}

	result, err := VerifyLicense(cfg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Payload.AppID != fx.appID {
		t.Errorf("app_id mismatch")
	}
	if result.Payload.MachineCode != fx.machineCode {
		t.Errorf("machine_code mismatch")
	}
	if result.PubKeySource != "env:LICENSE_PUBKEY_PEM" {
		t.Errorf("source=%q", result.PubKeySource)
	}
}

func TestVerifyLicense_WrongMachineCode(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	cfg := LicenseConfig{
		AppID:       fx.appID,
		MachineCode: "WRONG_MACHINE",
		LicenseKey:  fx.licenseKey,
	}
	// 错误根源：HKDF 用错 salt → AES key 错 → GCM Open 失败
	if _, err := VerifyLicense(cfg); err == nil {
		t.Error("expected error when machine_code differs")
	}
}

func TestVerifyLicense_WrongAppID(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	cfg := LicenseConfig{
		AppID:       "wrong_app",
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	}
	// 错误根源：HKDF salt 错 → AES key 错 → GCM Open 失败
	if _, err := VerifyLicense(cfg); err == nil {
		t.Error("expected error when app_id differs")
	}
}

func TestVerifyLicense_WrongPubKey(t *testing.T) {
	fx := newFixture(t, 30*86400)

	// 生成另一对 RSA 密钥 → 公钥不匹配
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherPKIX, _ := x509.MarshalPKIXPublicKey(&other.PublicKey)
	otherPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherPKIX}))
	t.Setenv("LICENSE_PUBKEY_PEM", otherPEM)

	cfg := LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  fx.licenseKey,
	}
	if _, err := VerifyLicense(cfg); err == nil {
		t.Error("expected error with wrong public key")
	}
}

func TestVerifyLicense_TruncatedKey(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)

	cfg := LicenseConfig{
		AppID:       fx.appID,
		MachineCode: fx.machineCode,
		LicenseKey:  "AAAA", // 太短
	}
	if _, err := VerifyLicense(cfg); err == nil {
		t.Error("expected error for truncated license_key")
	}
}

// ============== ValidatePayload ==============

func TestValidatePayload_Valid(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	result, err := VerifyLicense(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	status, err := ValidatePayload(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
	}, result)
	if err != nil {
		t.Errorf("validate: %v", err)
	}
	if status != StatusValid {
		t.Errorf("status=%s want VALID", status)
	}
}

func TestValidatePayload_ExpiringSoon(t *testing.T) {
	fx := newFixture(t, 3*86400) // 3 天后过期
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	result, err := VerifyLicense(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	status, vErr := ValidatePayload(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
	}, result)
	// ExpiringSoon 不应报错
	if vErr != nil {
		t.Errorf("EXPIRING_SOON should not error: %v", vErr)
	}
	if status != StatusExpiringSoon {
		t.Errorf("status=%s want EXPIRING_SOON", status)
	}
}

func TestValidatePayload_Expired(t *testing.T) {
	fx := newFixture(t, -86400) // 已过期 1 天
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	result, err := VerifyLicense(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	status, vErr := ValidatePayload(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
	}, result)
	if vErr == nil {
		t.Error("EXPIRED should error")
	}
	if status != StatusExpired {
		t.Errorf("status=%s want EXPIRED", status)
	}
}

func TestValidatePayload_NilResult(t *testing.T) {
	if _, err := ValidatePayload(LicenseConfig{}, nil); err == nil {
		t.Error("expected error for nil result")
	}
}

// ============== VerifyStartup ==============

func TestVerifyStartup_Valid(t *testing.T) {
	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	out := VerifyStartup(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	if out.Err != nil {
		t.Errorf("expected no err, got: %v", out.Err)
	}
	if out.Status != StatusValid {
		t.Errorf("status=%s", out.Status)
	}
	if out.Result == nil {
		t.Error("Result should not be nil on success")
	}
}

func TestVerifyStartup_Expired(t *testing.T) {
	fx := newFixture(t, -86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	out := VerifyStartup(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	if out.Err == nil {
		t.Error("expected error for expired license")
	}
	if out.Status != StatusExpired {
		t.Errorf("status=%s", out.Status)
	}
}

func TestVerifyStartup_BadConfig(t *testing.T) {
	out := VerifyStartup(LicenseConfig{}) // 缺 appID / machineCode / licenseKey
	if out.Err == nil {
		t.Error("expected error for empty config")
	}
}

func TestFormatOutcome(t *testing.T) {
	if !strings.Contains(FormatOutcome(nil), "no outcome") {
		t.Error("nil should report 'no outcome'")
	}
	if !strings.Contains(FormatOutcome(&StartupOutcome{Err: errMock("x")}), "FAIL") {
		t.Error("error case should report FAIL")
	}

	fx := newFixture(t, 30*86400)
	t.Setenv("LICENSE_PUBKEY_PEM", fx.pubPEM)
	out := VerifyStartup(LicenseConfig{
		AppID: fx.appID, MachineCode: fx.machineCode,
		LicenseKey: fx.licenseKey,
	})
	s := FormatOutcome(out)
	if !strings.Contains(s, "OK") || !strings.Contains(s, "VALID") {
		t.Errorf("format string: %q", s)
	}
}

// errMock 是测试用的错误。
type errMock string

func (e errMock) Error() string { return string(e) }