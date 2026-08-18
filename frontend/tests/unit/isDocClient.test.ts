import { describe, it, expect, vi, afterEach } from "vitest";
import { isDocClient } from "@/utils/isDocClient";

describe("isDocClient", () => {
  const originalUserAgent = navigator.userAgent;

  afterEach(() => {
    vi.unstubAllGlobals();
    Object.defineProperty(navigator, "userAgent", {
      value: originalUserAgent,
      configurable: true
    });
  });

  function setUA(ua: string) {
    Object.defineProperty(navigator, "userAgent", {
      value: ua,
      configurable: true
    });
  }

  it("returns true when UA contains DocClient", () => {
    setUA("Mozilla/5.0 DocClient/1.0 NativeWebView");
    expect(isDocClient()).toBe(true);
  });

  it("returns false for normal Chrome UA", () => {
    setUA(
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
    );
    expect(isDocClient()).toBe(false);
  });

  it("returns false when navigator is undefined (SSR-style guard)", () => {
    const stub = vi.stubGlobal("navigator", undefined as any);
    try {
      expect(isDocClient()).toBe(false);
    } finally {
      stub.unstub?.();
    }
  });
});