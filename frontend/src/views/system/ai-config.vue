<script setup lang="ts">
import { onMounted, reactive, ref, computed, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  Plus,
  Connection,
  StarFilled,
  View,
  Hide,
  ArrowDown,
  Picture,
  CircleCheck,
  CircleClose,
  Warning,
  More
} from "@element-plus/icons-vue";
import {
  listAIConfigs,
  getAIConfig,
  createAIConfig,
  updateAIConfig,
  deleteAIConfig,
  setDefaultAIConfig,
  testAIConfig,
  toggleAIMultimodal,
  getAIMeta,
  type AIConfigItem,
  type AIConfigPayload
} from "@/api/ai_config";
import {
  PRESET_PROVIDERS,
  PROTOCOL_LABEL,
  type ProviderKey,
  type PresetProvider,
  type PresetModel
} from "@/types/ai-presets";

defineOptions({ name: "SystemAIConfig" });

const loading = ref(false);
const list = ref<AIConfigItem[]>([]);
const protocols = ref<Array<{ value: string; label: string }>>([]);

const dialogVisible = ref(false);
const dialogMode = ref<"create" | "edit">("create");
const editingId = ref<number | null>(null);
const activeTab = ref<"preset" | "custom">("preset");
const showKey = ref(false);
const testingIds = ref<Set<number>>(new Set());
const dialogSaving = ref(false);
const showAdvanced = ref(false);

const initialForm = () => ({
  name: "" as string,
  provider: "deepseek" as ProviderKey,
  protocol: "openai_chat",
  modelKey: "" as string,
  apiBase: "" as string,
  apiKey: "" as string,
  defaultParams: {
    temperature: 0.7,
    maxTokens: 2048,
    topP: 0.9
  },
  defaultParamsRaw: "" as string,
  isDefault: false as boolean
});

const form = reactive(initialForm());

const currentPreset = computed<PresetProvider | undefined>(() =>
  PRESET_PROVIDERS.find((p) => p.value === form.provider)
);

const currentModel = computed<PresetModel | undefined>(() => {
  const preset = currentPreset.value;
  if (!preset) return undefined;
  return preset.models.find((m) => m.key === form.modelKey);
});

function labelProtocol(val: string): string {
  return (PROTOCOL_LABEL as Record<string, string>)[val] ?? val;
}

const badgeColor: Record<string, "danger" | "success" | "info" | "warning" | "primary"> = {
  旗舰: "danger",
  主力: "success",
  免费: "info",
  极速: "warning",
  推理: "primary"
};

async function loadMeta() {
  try {
    const res = await getAIMeta();
    protocols.value = res.data?.protocols ?? [];
  } catch (err) {
    ElMessage.error(`元数据加载失败：${(err as Error).message}`);
  }
}

async function loadList() {
  loading.value = true;
  try {
    const res = await listAIConfigs();
    list.value = res.data?.list ?? [];
  } catch (err) {
    ElMessage.error(`加载失败：${(err as Error).message}`);
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  await loadMeta();
  await loadList();
});

watch(
  () => form.provider,
  (val) => {
    if (dialogMode.value !== "create") return;
    const preset = PRESET_PROVIDERS.find((p) => p.value === val);
    if (!preset) return;
    if (preset.defaultBase) form.apiBase = preset.defaultBase;
    const rec = preset.models.find((m) => m.recommended);
    if (rec) {
      form.modelKey = rec.key;
      form.protocol = rec.protocol;
    } else {
      form.modelKey = "";
      form.protocol = "openai_chat";
    }
  }
);

function openCreate() {
  dialogMode.value = "create";
  editingId.value = null;
  Object.assign(form, initialForm());
  showKey.value = false;
  activeTab.value = "preset";
  dialogVisible.value = true;
}

async function openEdit(row: AIConfigItem) {
  dialogMode.value = "edit";
  editingId.value = row.id;
  const full = await getAIConfig(row.id).catch(() => ({ data: { data: row } }));
  const cfg = (full as any).data?.data ?? row;

  const dp: Record<string, unknown> = {};
  try {
    Object.assign(dp, cfg.default_params ? JSON.parse(cfg.default_params) : {});
  } catch {}

  const baseParams = {
    temperature: typeof dp.temperature === "number" ? dp.temperature : 0.7,
    maxTokens: typeof dp.max_tokens === "number" ? dp.max_tokens : 2048,
    topP: typeof dp.top_p === "number" ? dp.top_p : 0.9
  };

  const reserved = new Set(["temperature", "max_tokens", "top_p"]);
  const rawObj = Object.fromEntries(
    Object.entries(dp).filter(([k]) => !reserved.has(k))
  );
  const defaultParamsRaw = Object.keys(rawObj).length ? JSON.stringify(rawObj, null, 2) : "";

  Object.assign(form, {
    name: cfg.name ?? "",
    provider: String(cfg.provider ?? "deepseek"),
    protocol: String(cfg.protocol ?? "openai_chat"),
    modelKey: cfg.model_name,
    apiBase: cfg.api_base,
    apiKey: "",
    defaultParams: baseParams,
    defaultParamsRaw,
    isDefault: !!cfg.is_default
  });

  showKey.value = false;
  showAdvanced.value = false;
  activeTab.value =
    form.provider === "custom" ? "custom" : PRESET_PROVIDERS.some((p) => p.value === form.provider) ? "preset" : "custom";
  dialogVisible.value = true;
}

async function handleSave() {
  const nameTrimmed = form.name?.trim() ?? "";
  if (nameTrimmed.length > 64) {
    ElMessage.warning("展示名不超过 64 字符");
    return;
  }
  if (!form.modelKey?.trim()) {
    ElMessage.warning("请选择或填写模型");
    return;
  }
  if (!form.apiBase?.trim()) {
    ElMessage.warning("请填写 API Base");
    return;
  }

  const isLocalProvider = form.provider === "ollama" || form.protocol === "ollama_chat";
  if (dialogMode.value === "create" && !isLocalProvider && !form.apiKey.trim()) {
    ElMessage.warning("首次创建必须填写 API Key");
    return;
  }

  dialogSaving.value = true;
  try {
    const payload: AIConfigPayload = {
      provider: activeTab.value === "custom" ? "custom" : form.provider,
      protocol: form.protocol,
      model_name: form.modelKey.trim(),
      api_base: form.apiBase.trim(),
      default_params: mergeDefaultParams(form.defaultParams, form.defaultParamsRaw),
      is_default: form.isDefault
    };

    if (nameTrimmed) payload.name = nameTrimmed;
    if (form.apiKey.trim()) payload.api_key = form.apiKey.trim();
    if (editingId.value) payload.id = editingId.value;

    const saved = dialogMode.value === "create"
      ? await createAIConfig(payload)
      : await updateAIConfig(editingId.value!, payload);

    ElMessage.success(dialogMode.value === "create" ? "创建成功" : "更新成功");
    dialogVisible.value = false;
    await loadList();

    const savedId = (saved as any).data?.id ?? editingId.value;
    const savedRow = list.value.find((r) => r.id === savedId);
    if (savedRow) {
      await handleTest(savedRow);
    }
  } catch (err) {
    ElMessage.error(`保存失败：${(err as Error).message}`);
  } finally {
    dialogSaving.value = false;
  }
}

async function handleDelete(row: AIConfigItem) {
  try {
    await ElMessageBox.confirm(
      `确认删除 AI 配置「${row.model_name}」？`,
      "删除确认",
      { type: "warning" }
    );
    await deleteAIConfig(row.id);
    ElMessage.success("删除成功");
    await loadList();
  } catch (err) {
    if ((err as Error).message?.includes("取消")) return;
    ElMessage.error(`删除失败：${(err as Error).message}`);
  }
}

async function handleSetDefault(row: AIConfigItem) {
  try {
    await setDefaultAIConfig(row.id);
    ElMessage.success(`已将「${row.model_name}」设为默认`);
    await loadList();
  } catch (err) {
    ElMessage.error(`操作失败：${(err as Error).message}`);
  }
}

async function handleTest(row: AIConfigItem) {
  if (testingIds.value.has(row.id)) return;
  testingIds.value.add(row.id);
  try {
    const res = await testAIConfig(row.id);
    ElMessage.success(`${res.message} (${res.latencyMs}ms)`);
    await loadList();
  } catch (err) {
    await loadList();
    ElMessage.error(`测试失败：${(err as Error).message}`);
  } finally {
    testingIds.value.delete(row.id);
  }
}

async function handleToggleMultimodal(row: AIConfigItem, supported: 0 | 1) {
  try {
    await toggleAIMultimodal(row.id, supported);
    ElMessage.success(supported === 1 ? "已手动标记为支持多模态" : "已手动标记为不支持多模态");
    await loadList();
  } catch (err) {
    ElMessage.error(`切换失败：${(err as Error).message}`);
  }
}

function getMultimodalTag(row: AIConfigItem) {
  if (row.multimodal_supported === null || row.multimodal_supported === undefined) return null;
  const isManual = row.multimodal_check_source === "manual";
  if (row.multimodal_supported === 1) {
    return { text: isManual ? "手动·支持" : "支持", type: "success" as const, effect: isManual ? "light" as const : "plain" as const };
  }
  return { text: isManual ? "手动·不支持" : "不支持", type: "danger" as const, effect: isManual ? "light" as const : "plain" as const };
}

function parseTestResult(json: string | null | undefined): { ok: boolean; latencyMs: number; message: string } | null {
  if (!json) return null;
  try {
    return JSON.parse(json);
  } catch {
    return null;
  }
}

function getTestFeedbackTag(row: AIConfigItem) {
  if (!row.test_result || !row.test_result_at) return null;
  const snap = parseTestResult(row.test_result);
  if (!snap) return null;
  return { ok: snap.ok, detail: snap.message, type: snap.ok ? "success" as const : "danger" as const, latencyMs: snap.latencyMs };
}

function formatTestAt(at: number | null | undefined): string {
  if (!at) return "";
  // 兼容旧数据：若时间戳看起来像秒（< 1e12），转换为毫秒
  const ts = at < 1_000_000_000_000 ? at * 1000 : at;
  const diff = Date.now() - ts;
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  return new Date(ts).toLocaleString("zh-CN");
}

function onPresetModelChange(key: string) {
  const preset = currentPreset.value;
  if (!preset) return;
  const m = preset.models.find((mm) => mm.key === key);
  if (m) form.protocol = m.protocol;
}

function mergeDefaultParams(base: Record<string, unknown>, raw: string): Record<string, unknown> {
  let rawObj: Record<string, unknown> = {};
  if (raw.trim()) {
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        rawObj = parsed as Record<string, unknown>;
      }
    } catch {}
  }
  return { ...rawObj, ...base };
}
</script>

<template>
  <div>
    <el-card v-loading="loading" shadow="never">
      <template #header>
        <div class="flex justify-between items-center">
          <span style="font-weight: 600">AI 配置</span>
          <el-button type="primary" @click="openCreate">
            <el-icon class="mr-1"><Plus /></el-icon>
            新建 AI 配置
          </el-button>
        </div>
      </template>

      <el-table :data="list" stripe>
        <el-table-column label="名称" min-width="250">
          <template #default="{ row }">
            <span v-if="row.name" style="font-weight: 600">{{ row.name }}</span>
            <span v-else class="muted-placeholder">未命名</span>
            <el-tooltip v-if="row.multimodal_supported === 1" content="支持多模态（图片识别）" placement="top">
              <el-icon class="ml-1 align-middle" color="var(--el-color-primary)" size="14">
                <Picture />
              </el-icon>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column label="模型名称" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <code class="model-name">{{ row.model_name }}</code>
          </template>
        </el-table-column>

        <el-table-column label="服务商" width="120">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ row.provider }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="协议" width="130">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ labelProtocol(String(row.protocol)) }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="默认" width="80" align="center">
          <template #default="{ row }">
            <el-tooltip v-if="row.is_default" content="当前默认配置">
              <el-icon color="#e6a23c" size="18"><StarFilled /></el-icon>
            </el-tooltip>
            <span v-else class="muted-placeholder">—</span>
          </template>
        </el-table-column>

        <el-table-column label="多模态" width="120" align="center">
          <template #default="{ row }">
            <el-tooltip v-if="getMultimodalTag(row as AIConfigItem)" :content="row.multimodal_checked_at ? `检测于 ${new Date(row.multimodal_checked_at).toLocaleString('zh-CN')}` : ''">
              <el-tag :type="getMultimodalTag(row as AIConfigItem)!.type" :effect="getMultimodalTag(row as AIConfigItem)!.effect" size="small">
                <el-icon class="mr-1" size="12">
                  <component :is="(row as AIConfigItem).multimodal_supported === 1 ? CircleCheck : CircleClose" />
                </el-icon>
                {{ getMultimodalTag(row as AIConfigItem)!.text }}
              </el-tag>
            </el-tooltip>
            <el-tooltip v-else content="点击「测试」自动检测，或右键手动标记">
              <el-tag type="info" effect="plain" size="small">
                <el-icon class="mr-1" size="12"><Warning /></el-icon>
                未检测
              </el-tag>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column label="上次测试" width="140" align="center">
          <template #default="{ row }">
            <el-tooltip v-if="getTestFeedbackTag(row as AIConfigItem)" :content="(getTestFeedbackTag(row as AIConfigItem) as any).detail" placement="top">
              <div class="flex items-center justify-center gap-2">
                <el-icon :color="getTestFeedbackTag(row as AIConfigItem)!.type === 'success' ? 'var(--el-color-success)' : 'var(--el-color-danger)'" size="16">
                  <component :is="getTestFeedbackTag(row as AIConfigItem)!.type === 'success' ? CircleCheck : CircleClose" />
                </el-icon>
                <span class="text-xs text-gray-500">{{ formatTestAt(row.test_result_at) }}</span>
              </div>
            </el-tooltip>
            <span v-else class="muted-placeholder">未测过</span>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <span class="op-cell">
              <el-button link type="primary" size="small" :loading="testingIds.has(row.id)" @click="handleTest(row as AIConfigItem)">
                测试
              </el-button>
              <el-button link type="primary" size="small" @click="openEdit(row as AIConfigItem)">
                编辑
              </el-button>
              <el-dropdown trigger="click" @command="(cmd: string) => {
                if (cmd === 'default') handleSetDefault(row as AIConfigItem);
                else if (cmd === 'delete') handleDelete(row as AIConfigItem);
                else if (cmd === 'mm-support') handleToggleMultimodal(row as AIConfigItem, 1);
                else if (cmd === 'mm-unsupport') handleToggleMultimodal(row as AIConfigItem, 0);
              }">
                <el-button type="primary" link size="small" class="op-more">
                  更多<el-icon class="el-icon--right"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item v-if="!(row as AIConfigItem).is_default" command="default">
                      设为默认
                    </el-dropdown-item>
                    <el-dropdown-item command="mm-support" :disabled="(row as AIConfigItem).multimodal_supported === 1 && (row as AIConfigItem).multimodal_check_source === 'manual'">
                      手动标记为支持多模态
                    </el-dropdown-item>
                    <el-dropdown-item command="mm-unsupport" :disabled="(row as AIConfigItem).multimodal_supported === 0 && (row as AIConfigItem).multimodal_check_source === 'manual'">
                      手动标记为不支持
                    </el-dropdown-item>
                    <el-dropdown-item command="delete" divided style="color: var(--el-color-danger)">
                      删除
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </span>
          </template>
        </el-table-column>

        <template #empty>
          <el-empty description="暂无 AI 配置" />
        </template>
      </el-table>
    </el-card>

    <!-- 新建/编辑弹窗 -->
    <el-dialog v-model="dialogVisible" :title="dialogMode === 'create' ? '新建 AI 配置' : '编辑 AI 配置'" width="880px" destroy-on-close>
      <el-tabs v-model="activeTab">
        <el-tab-pane label="服务商预设" name="preset">
          <el-form label-width="100px">
            <el-row :gutter="16">
              <el-col :span="14">
                <el-form-item label="名称">
                  <el-input v-model="form.name" placeholder="留空自动生成" maxlength="64" clearable />
                </el-form-item>
              </el-col>
              <el-col :span="10">
                <el-form-item label="默认配置">
                  <el-switch v-model="form.isDefault" />
                </el-form-item>
              </el-col>
            </el-row>

            <el-form-item label="服务商" required>
              <el-select v-model="form.provider" placeholder="选择服务商" style="width: 100%">
                <el-option v-for="p in PRESET_PROVIDERS" :key="p.value" :label="p.label" :value="p.value" />
              </el-select>
              <div v-if="currentPreset" class="text-xs text-gray-500 mt-1">{{ currentPreset.desc }}</div>
            </el-form-item>

            <el-form-item v-if="currentPreset && currentPreset.models.length > 0" label="预制模型" required>
              <el-select v-model="form.modelKey" placeholder="选择模型" filterable style="width: 100%" @change="onPresetModelChange">
                <el-option v-for="m in currentPreset.models" :key="m.key" :value="m.key" :label="`${m.displayName}${m.badge ? ` · ${m.badge}` : ''}`">
                  <div class="flex items-center justify-between" style="width: 100%">
                    <div>
                      <span style="font-weight: 600">{{ m.displayName }}</span>
                      <el-tag v-if="m.badge" size="small" :type="badgeColor[m.badge] ?? 'info'" effect="plain" class="ml-2">{{ m.badge }}</el-tag>
                      <div class="text-xs text-gray-500 mt-1">入 {{ m.inputTokens.toLocaleString() }} / 出 {{ m.outputTokens.toLocaleString() }} tokens</div>
                      <div v-if="m.desc" class="text-xs text-gray-400">{{ m.desc }}</div>
                    </div>
                  </div>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item v-else label="模型键" required>
              <el-input v-model="form.modelKey" placeholder="如 my-custom-model" />
            </el-form-item>

            <el-form-item label="协议">
              <el-tag effect="plain" size="small">{{ labelProtocol(form.protocol) }}</el-tag>
            </el-form-item>

            <el-form-item label="API Base" required>
              <el-input v-model="form.apiBase" placeholder="切换服务商时会自动填入" />
            </el-form-item>

            <el-form-item label="API Key">
              <el-input v-model="form.apiKey" :type="showKey ? 'text' : 'password'" :placeholder="form.provider === 'ollama' ? 'Ollama 本地免鉴权' : dialogMode === 'edit' ? '留空表示不修改' : 'sk-...'">
                <template #suffix>
                  <el-icon class="cursor-pointer" @click="showKey = !showKey">
                    <component :is="showKey ? View : Hide" />
                  </el-icon>
                </template>
              </el-input>
              <div v-if="form.provider === 'ollama'" class="text-xs text-gray-400 mt-1">本地 Ollama 无需鉴权</div>
            </el-form-item>

            <el-form-item>
              <el-button link type="primary" class="!text-sm" @click="showAdvanced = !showAdvanced">
                <el-icon class="mr-1"><component :is="ArrowDown" :style="{ transform: showAdvanced ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }" /></el-icon>
                {{ showAdvanced ? '收起高级' : '高级' }}
              </el-button>
            </el-form-item>

            <template v-if="showAdvanced">
              <el-form-item v-if="currentModel" label="上下文窗口">
                <el-descriptions :column="2" size="small" border>
                  <el-descriptions-item label="输入 Tokens">{{ currentModel.inputTokens.toLocaleString() }}</el-descriptions-item>
                  <el-descriptions-item label="输出 Tokens">{{ currentModel.outputTokens.toLocaleString() }}</el-descriptions-item>
                </el-descriptions>
              </el-form-item>

              <el-form-item label="默认参数">
                <div class="grid grid-cols-3 gap-2 w-full">
                  <div>
                    <div class="text-xs text-gray-500 mb-1">温度</div>
                    <el-input-number v-model="form.defaultParams.temperature" :min="0" :max="2" :step="0.1" :precision="2" size="small" style="width: 100%" />
                  </div>
                  <div>
                    <div class="text-xs text-gray-500 mb-1">最大输出</div>
                    <el-input-number v-model="form.defaultParams.maxTokens" :min="1" :max="128000" :step="512" size="small" style="width: 100%" />
                  </div>
                  <div>
                    <div class="text-xs text-gray-500 mb-1">Top-P</div>
                    <el-input-number v-model="form.defaultParams.topP" :min="0" :max="1" :step="0.05" :precision="2" size="small" style="width: 100%" />
                  </div>
                </div>
              </el-form-item>

              <el-form-item label="高级参数">
                <el-input v-model="form.defaultParamsRaw" type="textarea" :rows="4" placeholder='JSON 自由扩展，如 {"frequency_penalty":0.5}' />
              </el-form-item>
            </template>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="自定义配置" name="custom">
          <el-form label-width="100px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="名称">
                  <el-input v-model="form.name" placeholder="留空自动生成" maxlength="64" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="默认配置">
                  <el-switch v-model="form.isDefault" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="协议" required>
              <el-select v-model="form.protocol" placeholder="选择协议" style="width: 100%">
                <el-option v-for="p in protocols" :key="p.value" :label="p.label" :value="p.value" />
              </el-select>
            </el-form-item>
            <el-form-item label="模型键" required>
              <el-input v-model="form.modelKey" placeholder="如 my-custom-model-v1" />
            </el-form-item>
            <el-form-item label="API Base" required>
              <el-input v-model="form.apiBase" placeholder="https://your-gateway.com/v1" />
            </el-form-item>
            <el-form-item label="API Key">
              <el-input v-model="form.apiKey" :type="showKey ? 'text' : 'password'" :placeholder="form.protocol === 'ollama_chat' ? 'Ollama 本地免鉴权' : dialogMode === 'edit' ? '留空表示不修改' : 'sk-...'">
                <template #suffix>
                  <el-icon class="cursor-pointer" @click="showKey = !showKey">
                    <component :is="showKey ? View : Hide" />
                  </el-icon>
                </template>
              </el-input>
            </el-form-item>
            <el-form-item label="默认参数">
              <el-input v-model="form.defaultParamsRaw" type="textarea" :rows="6" placeholder='{"temperature":0.7,"max_tokens":2048}' />
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="dialogSaving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.model-name {
  font-family: ui-monospace, "SF Mono", Consolas, monospace;
  font-size: 12px;
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 3px;
  color: var(--el-text-color-regular);
}
.muted-placeholder {
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}
</style>
