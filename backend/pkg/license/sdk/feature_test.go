package sdk

import (
	"testing"
	"time"
)

// makeOutcome 构造一个测试用 StartupOutcome（包外 helper，避免污染包内导出符号）。
func makeOutcome(features []string, expiresAt int64) *StartupOutcome {
	return &StartupOutcome{
		Result: &DecryptResult{
			Payload: LicensePayload{
				AppID:       "doc_crm_v1",
				MachineCode: "TEST_MACHINE",
				Features:    features,
				IssuedAt:    time.Now().Unix(),
				ExpiresAt:   expiresAt,
				Algo:        "rsa2048-sha256+aes256gcm-hkdf",
			},
		},
		Status:    StatusValid,
		PubKeySrc: "test",
	}
}

// TestHasFeature_NilOutcome nil → false（不 panic）。
func TestHasFeature_NilOutcome(t *testing.T) {
	if HasFeature(nil, "rag") {
		t.Errorf("nil outcome should return false")
	}
}

// TestHasFeature_NilResult result==nil → false。
func TestHasFeature_NilResult(t *testing.T) {
	o := &StartupOutcome{Result: nil}
	if HasFeature(o, "rag") {
		t.Errorf("nil result should return false")
	}
}

// TestHasFeature_EmptyFeatures features=[]string{} → false（你的硬约束）。
func TestHasFeature_EmptyFeatures(t *testing.T) {
	o := makeOutcome([]string{}, time.Now().Add(24*time.Hour).Unix())
	if HasFeature(o, "rag") {
		t.Errorf("empty features should return false")
	}
}

// TestHasFeature_NilFeatures features=nil → false。
func TestHasFeature_NilFeatures(t *testing.T) {
	o := makeOutcome(nil, time.Now().Add(24*time.Hour).Unix())
	if HasFeature(o, "rag") {
		t.Errorf("nil features should return false")
	}
}

// TestHasFeature_ExactHit 命中精确字符串相等。
func TestHasFeature_ExactHit(t *testing.T) {
	o := makeOutcome([]string{"rag"}, time.Now().Add(24*time.Hour).Unix())
	if !HasFeature(o, "rag") {
		t.Errorf("[\"rag\"] should hit \"rag\"")
	}
}

// TestHasFeature_MultiHit 多元素数组里命中其中一个。
func TestHasFeature_MultiHit(t *testing.T) {
	o := makeOutcome([]string{"ai", "rag", "ocr"}, time.Now().Add(24*time.Hour).Unix())
	if !HasFeature(o, "rag") {
		t.Errorf("[ai,rag,ocr] should hit \"rag\"")
	}
	if !HasFeature(o, "ocr") {
		t.Errorf("[ai,rag,ocr] should hit \"ocr\"")
	}
}

// TestHasFeature_SubstringMustFail 子串前缀不能命中（精确匹配）。
func TestHasFeature_SubstringMustFail(t *testing.T) {
	o := makeOutcome([]string{"ragi"}, time.Now().Add(24*time.Hour).Unix())
	if HasFeature(o, "rag") {
		t.Errorf("[\"ragi\"] must NOT hit \"rag\" (prefix/substring must fail)")
	}
}

// TestHasFeature_CaseAndSpaceMustFail 大小写/前后空格都不能命中。
func TestHasFeature_CaseAndSpaceMustFail(t *testing.T) {
	o := makeOutcome([]string{"RAG", " rag ", "rag "}, time.Now().Add(24*time.Hour).Unix())
	if HasFeature(o, "rag") {
		t.Errorf("[\"RAG\",\" rag \",\"rag \"] must NOT hit \"rag\" (no normalize)")
	}
}

// TestHasFeature_CurrentLicenseDocCRM 当前 DocCRM dev license payload: ["rag","ai"]。
// RAG ✅ ; 假设未来加 "ocr"（业务实际消费）应 ❌ ; 假设完全无关 "xyz" 应 ❌。
func TestHasFeature_CurrentLicenseDocCRM(t *testing.T) {
	o := makeOutcome([]string{"rag", "ai"}, time.Now().Add(24*time.Hour).Unix())
	if !HasFeature(o, "rag") {
		t.Errorf("dev license should hit rag")
	}
	if HasFeature(o, "ocr") {
		t.Errorf("dev license must NOT hit ocr")
	}
	if HasFeature(o, "xyz") {
		t.Errorf("dev license must NOT hit xyz")
	}
}

// TestGlobalHasFeature_NilBeforeSet Set 之前 GlobalHasFeature → false。
func TestGlobalHasFeature_NilBeforeSet(t *testing.T) {
	prev := globalOutcome.Load()
	globalOutcome.Store(nil)
	defer globalOutcome.Store(prev)
	if GlobalHasFeature("rag") {
		t.Errorf("before SetGlobalOutcome, GlobalHasFeature should be false")
	}
}

// TestGlobalHasFeature_AfterSetGlobal 写后读：原子可见。
func TestGlobalHasFeature_AfterSetGlobal(t *testing.T) {
	prev := globalOutcome.Load()
	defer globalOutcome.Store(prev)

	o := makeOutcome([]string{"rag"}, time.Now().Add(24*time.Hour).Unix())
	SetGlobalOutcome(o)
	if !GlobalHasFeature("rag") {
		t.Errorf("after SetGlobalOutcome([\"rag\"]), GlobalHasFeature(\"rag\") should be true")
	}
	if GlobalHasFeature("ocr") {
		t.Errorf("after SetGlobalOutcome([\"rag\"]), GlobalHasFeature(\"ocr\") should be false")
	}
}
