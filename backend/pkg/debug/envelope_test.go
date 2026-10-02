package debug

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"doc/pkg/license/crypto"
)

// fixedClock 是测试用 mock Clock。
//
// 行为：始终返回构造时指定的固定时间（Unix 秒）。
// 用途：让窗口判定结果可重现，不依赖系统时间。
type fixedClock struct{ now int64 }

func (f fixedClock) NowUnix() int64 { return f.now }

// 复现签发端：构造 envelope 并返回 base64 字符串。
//
//	aes_key      = HKDF-SHA256(salt=app_id, ikm=password, info="lmp-client-request", L=32)
//	envelope_b64 = base64_urlsafe(nonce(12) || AES-GCM-Seal(plaintext_json, AAD=nil))
func mintEnvelope(t *testing.T, appID, password string, payload DebugPayload) string {
	t.Helper()
	pt, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return sealEnvelope(t, appID, password, pt)
}

// sealEnvelope 是 envelope 构造的最低层（接受原始字节，#6 修复：去重 AES-GCM 构造代码）。
//
// 测试中需要构造非法 JSON 内容时，直接传 raw bytes 即可（跳过 DebugPayload 序列化）。
func sealEnvelope(t *testing.T, appID, password string, plaintext []byte) string {
	t.Helper()

	key, err := crypto.DeriveAesKey(appID, password, HKDFInfo)
	if err != nil {
		t.Fatalf("derive AES key: %v", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes new cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm new: %v", err)
	}

	nonce := make([]byte, crypto.AESNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("read nonce: %v", err)
	}
	ctWithTag := aead.Seal(nil, nonce, plaintext, nil) // 协议：无 AAD

	env := make([]byte, 0, crypto.AESNonceLen+len(ctWithTag))
	env = append(env, nonce...)
	env = append(env, ctWithTag...)
	return base64.URLEncoding.EncodeToString(env) // 协议：URL-safe base64
}

func TestResolveDebugMode_Absent(t *testing.T) {
	out := ResolveDebugMode(DebugConfig{
		AppID:       "test_app",
		Password:    "secret",
		DebugEnvB64: "",
	})
	if out.Active {
		t.Errorf("expected Active=false, got true")
	}
	if out.Source != SourceAbsent {
		t.Errorf("expected Source=SourceAbsent, got %q", out.Source)
	}
}

func TestResolveDebugMode_NoCredentials(t *testing.T) {
	// 信封有值但 AppID/Password 都为空 → 等同 absent（不报错）
	out := ResolveDebugMode(DebugConfig{
		AppID:       "",
		Password:    "",
		DebugEnvB64: "anything",
	})
	if out.Active {
		t.Errorf("expected Active=false, got true")
	}
	if out.Source != SourceAbsent {
		t.Errorf("expected Source=SourceAbsent, got %q", out.Source)
	}
}

func TestResolveDebugMode_DecryptFailed(t *testing.T) {
	out := ResolveDebugMode(DebugConfig{
		AppID:       "test_app",
		Password:    "secret",
		DebugEnvB64: "this-is-not-base64-!!!",
	})
	if out.Active {
		t.Errorf("expected Active=false, got true")
	}
	if out.Source != SourceDecryptFailed {
		t.Errorf("expected Source=SourceDecryptFailed, got %q", out.Source)
	}
	if out.Err == nil {
		t.Errorf("expected Err != nil")
	}
}

func TestResolveDebugMode_BadWindow_BeforeStart(t *testing.T) {
	const appID = "test_app"
	const pwd = "secret"

	env := mintEnvelope(t, appID, pwd, DebugPayload{
		StartTime: "2026-09-23T10:00:00Z", // 未来
		EndTime:   "2026-09-23T12:00:00Z",
		Issued:    999,
	})

	out := ResolveDebugMode(DebugConfig{
		AppID:       appID,
		Password:    pwd,
		DebugEnvB64: env,
		Clock:       fixedClock{now: 1000000}, // 1970-01-12，窗口未开始
	})
	if out.Active {
		t.Errorf("expected Active=false (before window), got true")
	}
	if out.Source != SourceBadWindow {
		t.Errorf("expected Source=SourceBadWindow, got %q", out.Source)
	}
}

func TestResolveDebugMode_BadWindow_AfterEnd(t *testing.T) {
	const appID = "test_app"
	const pwd = "secret"

	env := mintEnvelope(t, appID, pwd, DebugPayload{
		StartTime: "2026-09-23T10:00:00Z",
		EndTime:   "2026-09-23T12:00:00Z", // 早于 now
		Issued:    999,
	})

	out := ResolveDebugMode(DebugConfig{
		AppID:       appID,
		Password:    pwd,
		DebugEnvB64: env,
		Clock:       fixedClock{now: 2000000000}, // 2033-05
	})
	if out.Active {
		t.Errorf("expected Active=false (after window), got true")
	}
	if out.Source != SourceBadWindow {
		t.Errorf("expected Source=SourceBadWindow, got %q", out.Source)
	}
}

func TestResolveDebugMode_Active(t *testing.T) {
	const appID = "test_app"
	const pwd = "secret"

	env := mintEnvelope(t, appID, pwd, DebugPayload{
		StartTime: "2025-09-27T00:00:00Z",
		EndTime:   "2025-09-29T00:00:00Z",
		Issued:    42,
	})

	out := ResolveDebugMode(DebugConfig{
		AppID:       appID,
		Password:    pwd,
		DebugEnvB64: env,
		Clock:       fixedClock{now: 1759000000}, // ≈ 2025-09-28
	})
	if !out.Active {
		t.Errorf("expected Active=true, got false (source=%s, err=%v)", out.Source, out.Err)
	}
	if out.Source != SourceActive {
		t.Errorf("expected Source=SourceActive, got %q", out.Source)
	}
	if out.Issued != 42 {
		t.Errorf("expected Issued=42, got %d", out.Issued)
	}
}

func TestResolveDebugMode_DecodeFailed_BadJSON(t *testing.T) {
	const appID = "test_app"
	const pwd = "secret"

	// 构造一个能解密成功但 JSON 不是合法 DebugPayload 的 envelope。
	// 直接复用 sealEnvelope，跳过 DebugPayload 序列化（#6 修复）。
	env := sealEnvelope(t, appID, pwd, []byte(`{"foo":"bar"}`))

	out := ResolveDebugMode(DebugConfig{
		AppID:       appID,
		Password:    pwd,
		DebugEnvB64: env,
	})
	if out.Active {
		t.Errorf("expected Active=false, got true")
	}
	// 字段缺失 → time.Parse 报错 → SourceDecodeFailed
	if out.Source != SourceDecodeFailed {
		t.Errorf("expected Source=SourceDecodeFailed, got %q", out.Source)
	}
}

func TestResolveDebugMode_WrongPassword(t *testing.T) {
	// 用密码 A 签发，但用密码 B 解密 → SourceDecryptFailed（AES-GCM tag mismatch）
	env := mintEnvelope(t, "test_app", "secret_A", DebugPayload{
		StartTime: "2025-09-27T00:00:00Z",
		EndTime:   "2025-09-29T00:00:00Z",
		Issued:    1,
	})

	out := ResolveDebugMode(DebugConfig{
		AppID:       "test_app",
		Password:    "secret_B", // 故意错
		DebugEnvB64: env,
	})
	if out.Active {
		t.Errorf("expected Active=false, got true")
	}
	if out.Source != SourceDecryptFailed {
		t.Errorf("expected Source=SourceDecryptFailed, got %q", out.Source)
	}
}

func TestResolveDebugMode_DefaultClock(t *testing.T) {
	// 不传 Clock 字段 → 自动降级为 SystemClock。
	// 窗口设为 1970-01-01 ~ 1970-01-02 → 必然不在系统时间窗口内，
	// 验证 nil Clock 路径不会 panic，且能正常返回 SourceBadWindow。
	env := mintEnvelope(t, "test_app", "secret", DebugPayload{
		StartTime: "1970-01-01T00:00:00Z",
		EndTime:   "1970-01-02T00:00:00Z",
		Issued:    0,
	})

	out := ResolveDebugMode(DebugConfig{
		AppID:       "test_app",
		Password:    "secret",
		DebugEnvB64: env,
		// Clock 不传 → SystemClock{}
	})
	if out.Active {
		t.Errorf("expected Active=false (system time far past 1970 window), got true")
	}
	if out.Source != SourceBadWindow {
		t.Errorf("expected Source=SourceBadWindow, got %q", out.Source)
	}
}

// goldenEnvelopeB64 是 2026-09-23 由 LMP 签发端真实下发、存于
// bin/config.test-main.json 顶层 "debug" 字段的信封（URL-safe base64）。
// 明文（窗口内）：
//
//	{"endtime":"2026-09-26T03:33:00Z","issued":1790134414,
//	 "starttime":"2026-09-23T03:33:00Z"}
const goldenEnvelopeB64 = "VjkCyyDD0JWdOITUNeEHgJ9whwNiTx8V1Bp2YweQGGwSHRyMyaRV8PtJLSn01QITZeyixdD5joJFUREXPjFedCQE4jmKcAqYcv8tMufVHpOSadciJl2ZhQFfvL826NR6UQRadxa0LQgfbHW4XEaFwN-eFH9c"

// TestResolveDebugMode_GoldenRealEnvelope 用真实签发信封做端到端验证，
// 防止"测试自签自解、协议参数错误也全绿"（2026-09-23 第三轮审查的实际事故）。
//
// password 是 license payload 的敏感字段，不硬编码入仓库，
// 通过 DEBUG_GOLDEN_PASSWORD 注入；未设置时 skip（CI 默认 skip）。
// 本地实证：
//
//	$env:DEBUG_GOLDEN_PASSWORD = (go run ./cmd/license_dump ... 输出的 password)
//	go test ./pkg/debug/ -run GoldenRealEnvelope -v
func TestResolveDebugMode_GoldenRealEnvelope(t *testing.T) {
	password := os.Getenv("DEBUG_GOLDEN_PASSWORD")
	if password == "" {
		t.Skip("DEBUG_GOLDEN_PASSWORD 未设置，跳过真实信封 golden 向量")
	}

	// 固定在窗口中部：2026-09-24T00:00:00Z
	clock := fixedClock{now: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC).Unix()}

	out := ResolveDebugMode(DebugConfig{
		AppID:       "doc_crm_v1",
		Password:    password,
		DebugEnvB64: goldenEnvelopeB64,
		Clock:       clock,
	})
	if !out.Active {
		t.Fatalf("expected Active=true; source=%s err=%v", out.Source, out.Err)
	}
	if out.Source != SourceActive {
		t.Errorf("expected Source=SourceActive, got %q", out.Source)
	}
	if out.Issued != 1790134414 {
		t.Errorf("expected Issued=1790134414, got %d", out.Issued)
	}
	wantStart := time.Date(2026, 9, 23, 3, 33, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 26, 3, 33, 0, 0, time.UTC)
	if !out.StartTime.Equal(wantStart) {
		t.Errorf("expected StartTime=%v, got %v", wantStart, out.StartTime)
	}
	if !out.EndTime.Equal(wantEnd) {
		t.Errorf("expected EndTime=%v, got %v", wantEnd, out.EndTime)
	}
}

// TestResolveDebugMode_GoldenEnvelopeExpired 同一真实信封，时钟固定在窗口外
// （2026-10-01）：解密必须成功但窗口判定失败 → SourceBadWindow。
// 用于区分"协议解不开"和"只是过期"。
func TestResolveDebugMode_GoldenEnvelopeExpired(t *testing.T) {
	password := os.Getenv("DEBUG_GOLDEN_PASSWORD")
	if password == "" {
		t.Skip("DEBUG_GOLDEN_PASSWORD 未设置，跳过真实信封 golden 向量")
	}

	clock := fixedClock{now: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix()}

	out := ResolveDebugMode(DebugConfig{
		AppID:       "doc_crm_v1",
		Password:    password,
		DebugEnvB64: goldenEnvelopeB64,
		Clock:       clock,
	})
	if out.Active {
		t.Errorf("expected Active=false (window expired), got true")
	}
	if out.Source != SourceBadWindow {
		t.Errorf("expected Source=SourceBadWindow (decrypt OK but expired), got %q err=%v", out.Source, out.Err)
	}
}