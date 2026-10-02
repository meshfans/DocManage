package crypto

import (
	stdcrypto "crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

// ============= PEM 解析测试 =============

func TestParseRSAPublicKey_PKIX(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pkixBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pkix: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pkixBytes,
	}))

	pub, err := ParseRSAPublicKey(pemStr)
	if err != nil {
		t.Fatalf("parse pkix: %v", err)
	}
	if pub.N.Cmp(priv.PublicKey.N) != 0 {
		t.Errorf("parsed N != original N")
	}
}

func TestParseRSAPublicKey_PKCS1(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pkcs1Bytes := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	pemStr := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pkcs1Bytes,
	}))

	pub, err := ParseRSAPublicKey(pemStr)
	if err != nil {
		t.Fatalf("parse pkcs1: %v", err)
	}
	if pub.N.Cmp(priv.PublicKey.N) != 0 {
		t.Errorf("parsed N != original N")
	}
}

func TestParseRSAPublicKey_Invalid(t *testing.T) {
	cases := []string{
		"",                                  // 空
		"not a pem",                         // 完全没有 PEM 头
		"-----BEGIN PUBLIC KEY-----\nxxx", // PKIX 解析失败
	}
	for _, c := range cases {
		if _, err := ParseRSAPublicKey(c); err == nil {
			t.Errorf("expected error for input %q", c)
		}
	}
}

func TestLooksLikePEM(t *testing.T) {
	if !looksLikePEM("-----BEGIN PUBLIC KEY-----\nfoo") {
		t.Error("expected true")
	}
	if !looksLikePEM("  -----BEGIN RSA PUBLIC KEY-----") {
		t.Error("expected true with leading whitespace")
	}
	if looksLikePEM("hello") {
		t.Error("expected false")
	}
}

// ============= HKDF 测试 =============

func TestDeriveAesKey_Deterministic(t *testing.T) {
	k1, err := DeriveAesKey("doc_crm_v1", "MACHINE123", HKDFInfo)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	k2, err := DeriveAesKey("doc_crm_v1", "MACHINE123", HKDFInfo)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if string(k1) != string(k2) {
		t.Error("HKDF not deterministic")
	}
	if len(k1) != HKDFOutputLen {
		t.Errorf("key length=%d, expected %d", len(k1), HKDFOutputLen)
	}
}

func TestDeriveAesKey_Sensitivity(t *testing.T) {
	// 改变 appID → key 不同
	k1, _ := DeriveAesKey("doc_crm_v1", "MACHINE123", "info")
	k2, _ := DeriveAesKey("doc_crm_v2", "MACHINE123", "info")
	if string(k1) == string(k2) {
		t.Error("salt (appID) change should produce different key")
	}

	// 改变 machine_code → key 不同
	k3, _ := DeriveAesKey("doc_crm_v1", "MACHINE123", "info")
	k4, _ := DeriveAesKey("doc_crm_v1", "MACHINE456", "info")
	if string(k3) == string(k4) {
		t.Error("ikm (machine_code) change should produce different key")
	}

	// 改变 info → key 不同
	k5, _ := DeriveAesKey("doc_crm_v1", "MACHINE123", "info")
	k6, _ := DeriveAesKey("doc_crm_v1", "MACHINE123", "info2")
	if string(k5) == string(k6) {
		t.Error("info change should produce different key")
	}
}

func TestDeriveAesKey_RejectsEmpty(t *testing.T) {
	if _, err := DeriveAesKey("", "m", "info"); err == nil {
		t.Error("expected error for empty appID")
	}
	if _, err := DeriveAesKey("a", "", "info"); err == nil {
		t.Error("expected error for empty machineCode")
	}
	if _, err := DeriveAesKey("a", "m", ""); err == nil {
		t.Error("expected error for empty info")
	}
}

// ============= AES-GCM 测试（round-trip + 故障注入）============

func aesRoundTrip(t *testing.T, key []byte, plaintext []byte) string {
	t.Helper()
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, AESNonceLen)
	_, _ = rand.Read(nonce)
	ct := aead.Seal(nil, nonce, plaintext, []byte(HKDFInfo))

	envelope := make([]byte, 0, len(nonce)+len(ct))
	envelope = append(envelope, nonce...)
	envelope = append(envelope, ct...)
	return base64.StdEncoding.EncodeToString(envelope)
}

func TestAesGcmDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, AESKeyLen)
	_, _ = rand.Read(key)

	plaintext := []byte(`{"app_id":"doc_crm_v1","features":["crm","audit"]}`)
	envB64 := aesRoundTrip(t, key, plaintext)

	out, err := AesGcmDecrypt(envB64, key, HKDFInfo)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(out) != string(plaintext) {
		t.Errorf("plaintext mismatch:\n got=%q\nwant=%q", out, plaintext)
	}
}

func TestAesGcmDecrypt_AADMismatch(t *testing.T) {
	key := make([]byte, AESKeyLen)
	_, _ = rand.Read(key)

	plaintext := []byte("hello")
	envB64 := aesRoundTrip(t, key, plaintext)

	if _, err := AesGcmDecrypt(envB64, key, "wrong-aad"); err == nil {
		t.Error("expected error for AAD mismatch")
	}
}

func TestAesGcmDecrypt_WrongKey(t *testing.T) {
	key1 := make([]byte, AESKeyLen)
	key2 := make([]byte, AESKeyLen)
	_, _ = rand.Read(key1)
	_, _ = rand.Read(key2)

	plaintext := []byte("hello")
	envB64 := aesRoundTrip(t, key1, plaintext)

	if _, err := AesGcmDecrypt(envB64, key2, HKDFInfo); err == nil {
		t.Error("expected error for wrong key")
	}
}

func TestAesGcmDecrypt_TruncatedEnvelope(t *testing.T) {
	key := make([]byte, AESKeyLen)
	_, _ = rand.Read(key)

	if _, err := AesGcmDecrypt("AAAA", key, HKDFInfo); err == nil {
		t.Error("expected error for too-short envelope")
	}
}

func TestAesGcmDecrypt_BadBase64(t *testing.T) {
	key := make([]byte, AESKeyLen)
	_, _ = rand.Read(key)

	if _, err := AesGcmDecrypt("not!base64!!!", key, HKDFInfo); err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestAesGcmDecrypt_BadKeyLen(t *testing.T) {
	// AES-128 key 不应被接受
	shortKey := make([]byte, 16)
	_, _ = rand.Read(shortKey)
	if _, err := AesGcmDecrypt("AAAA", shortKey, HKDFInfo); err == nil {
		t.Error("expected error for AES-128 key")
	}
}

// ============= RSA 验签测试（round-trip + 故障注入）============

func TestRsaVerifyPayload_RoundTrip(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	data := []byte(`{"app_id":"doc_crm_v1","machine_code":"M1"}`)

	hashed := sha256.Sum256(data)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, stdcrypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if err := RsaVerifyPayload(&priv.PublicKey, data, sig); err != nil {
		t.Errorf("verify should pass: %v", err)
	}
}

func TestRsaVerifyPayload_WrongSig(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)

	data := []byte(`{"app_id":"doc_crm_v1"}`)
	hashed := sha256.Sum256(data)

	// 用 other 私钥签 → 验签必然失败
	badSig, _ := rsa.SignPKCS1v15(rand.Reader, other, stdcrypto.SHA256, hashed[:])
	if err := RsaVerifyPayload(&priv.PublicKey, data, badSig); err == nil {
		t.Error("expected verify failure with wrong-key signature")
	}
}

func TestRsaVerifyPayload_TamperedData(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	data := []byte(`{"app_id":"doc_crm_v1"}`)
	hashed := sha256.Sum256(data)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, stdcrypto.SHA256, hashed[:])

	// 篡改后再验签
	tampered := []byte(`{"app_id":"doc_crm_v2"}`)
	if err := RsaVerifyPayload(&priv.PublicKey, tampered, sig); err == nil {
		t.Error("expected verify failure on tampered data")
	}
}

func TestRsaVerifyPayload_BadSigLen(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	data := []byte("hello")

	// 故意用短签名（256 字节而非预期的 256 → 这里其实是正确的，但我们截断测试）
	shortSig := make([]byte, 100)
	if err := RsaVerifyPayload(&priv.PublicKey, data, shortSig); err == nil ||
		!strings.Contains(err.Error(), "signature length") {
		t.Errorf("expected signature length error, got: %v", err)
	}
}

func TestRsaVerifyPayload_NilArgs(t *testing.T) {
	if err := RsaVerifyPayload(nil, []byte("x"), []byte("y")); err == nil {
		t.Error("expected error for nil pubKey")
	}
	if err := RsaVerifyPayload(&rsa.PublicKey{}, nil, []byte("y")); err == nil {
		t.Error("expected error for empty data")
	}
	if err := RsaVerifyPayload(&rsa.PublicKey{}, []byte("x"), nil); err == nil {
		t.Error("expected error for empty sig")
	}
}

// cryptoSHA256 辅助函数已移除；统一直接用 stdcrypto.SHA256。