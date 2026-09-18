import { describe, it, expect } from "vitest";
import { PRESET_PROVIDERS } from "@/types/ai-presets";

// AI Presets 单测
describe("AI Presets 配置", () => {
  it("应该有预设服务商列表", () => {
    expect(PRESET_PROVIDERS).toBeDefined();
    expect(Array.isArray(PRESET_PROVIDERS)).toBe(true);
    expect(PRESET_PROVIDERS.length).toBeGreaterThan(0);
  });

  it("每个服务商应该有 label 和 value", () => {
    for (const provider of PRESET_PROVIDERS) {
      expect(provider.label).toBeDefined();
      expect(provider.value).toBeDefined();
      expect(typeof provider.label).toBe("string");
      expect(typeof provider.value).toBe("string");
    }
  });

  it("每个预设应该有必要的字段", () => {
    for (const preset of PRESET_PROVIDERS) {
      expect(preset.name).toBeUndefined(); // 实际没有 name 字段，用 label
      expect(preset.label).toBeDefined();
      expect(preset.desc).toBeDefined();
      expect(preset.defaultBase).toBeDefined();
      expect(preset.defaultPath).toBeDefined();
      expect(preset.models).toBeDefined();
      expect(Array.isArray(preset.models)).toBe(true);
    }
  });

  it("每个模型的 multimodal 字段应该存在", () => {
    for (const preset of PRESET_PROVIDERS) {
      for (const model of preset.models) {
        expect(model).toHaveProperty("multimodal");
        expect(typeof model.multimodal).toBe("boolean");
      }
    }
  });

  it("siliconflow 应该有 deepseek 模型", () => {
    const siliconflowPreset = PRESET_PROVIDERS.find((p) => p.value === "siliconflow");
    expect(siliconflowPreset).toBeDefined();

    const deepseekModel = siliconflowPreset?.models.find((m) => m.key.includes("DeepSeek"));
    expect(deepseekModel).toBeDefined();
    expect(deepseekModel?.multimodal).toBe(true);
  });

  it("kimi 应该有 k2 模型", () => {
    const kimiPreset = PRESET_PROVIDERS.find((p) => p.value === "kimi");
    expect(kimiPreset).toBeDefined();

    const k2Model = kimiPreset?.models.find((m) => m.key.includes("k2"));
    expect(k2Model).toBeDefined();
    expect(k2Model?.multimodal).toBe(true);
  });

  it("doubao 应该有 seed 模型", () => {
    const doubaoPreset = PRESET_PROVIDERS.find((p) => p.value === "doubao");
    expect(doubaoPreset).toBeDefined();

    const seedModel = doubaoPreset?.models.find((m) => m.key.includes("seed"));
    expect(seedModel).toBeDefined();
    expect(seedModel?.multimodal).toBe(true);
  });

  it("minimax 应该有 M3 模型", () => {
    const minimaxPreset = PRESET_PROVIDERS.find((p) => p.value === "minimax");
    expect(minimaxPreset).toBeDefined();

    const m3Model = minimaxPreset?.models.find((m) => m.key.includes("M3"));
    expect(m3Model).toBeDefined();
    expect(m3Model?.multimodal).toBe(true);
  });

  it("ollama 应该无 multimodal 模型（本地推理）", () => {
    const ollamaPreset = PRESET_PROVIDERS.find((p) => p.value === "ollama");
    expect(ollamaPreset).toBeDefined();

    for (const model of ollamaPreset?.models ?? []) {
      expect(model.multimodal).toBe(false);
    }
  });
});
