<script setup lang="ts">
import { ref, onMounted, computed, reactive, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { http } from "@/utils/http";
import {
  listSystemConfigs,
  updateSystemConfig,
  deleteSystemConfig,
  type ConfigMeta
} from "@/api/system_config";
import { useIsAdmin } from "@/composables/useIsAdmin";
import { hasPerms } from "@/utils/auth";

defineOptions({ name: "SystemConfig" });

const isAdmin = useIsAdmin();

const loading = ref(false);
const saving = ref(false);
const activeTab = ref("file");

// ========== 业务配置 tab 状态 ==========
const businessConfigs = ref<ConfigMeta[]>([]);
const businessLoading = ref(false);
const businessSaving = ref(false);
const businessEditDialog = reactive({
  visible: false,
  key: "",
  /** 值：文本框为 string，数字输入为 number */
  value: "" as string | number,
  defaultValue: "",
  description: "",
  valueType: "text" as "text" | "bool" | "enum" | "number",
  enumOptions: [] as string[]
});

async function loadBusinessConfigs() {
  businessLoading.value = true;
  try {
    const res = await listSystemConfigs();
    if (res.success) {
      businessConfigs.value = res.data || [];
      console.debug("[业务配置] 加载成功", res.data?.length, "项");
    } else {
      ElMessage.error("加载业务配置失败: " + (res.message || "未知错误"));
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "请求失败");
    console.error("[业务配置] API 调用失败", e);
  } finally {
    businessLoading.value = false;
  }
}

// 切到业务配置 tab 时确保已加载（兜底：onMounted 加载未完成就切 tab）
watch(activeTab, async newTab => {
  if (newTab === "business" && businessConfigs.value.length === 0) {
    await loadBusinessConfigs();
  }
});

function openBusinessEdit(row: ConfigMeta) {
  businessEditDialog.key = row.key;
  businessEditDialog.value = row.value;
  businessEditDialog.defaultValue = row.default_value;
  businessEditDialog.description = row.description;
  businessEditDialog.valueType = row.value_type || "text";
  businessEditDialog.enumOptions = row.enum_options || [];
  businessEditDialog.visible = true;
}

async function saveBusinessEdit() {
  if (!businessEditDialog.key) return;
  businessSaving.value = true;
  try {
    // 前后 trim（仅 text 类型；bool/enum/number 无需 trim）
    const rawValue = businessEditDialog.value;
    const saveValue = typeof rawValue === "string" ? rawValue.trim() : String(rawValue);
    const res = await updateSystemConfig(
      businessEditDialog.key,
      saveValue
    );
    if (res.success) {
      ElMessage.success("保存成功");
      businessEditDialog.visible = false;
      await loadBusinessConfigs();
    } else {
      ElMessage.error("保存失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "请求失败");
  } finally {
    businessSaving.value = false;
  }
}

async function resetBusinessToDefault(row: ConfigMeta) {
  try {
    await ElMessageBox.confirm(
      `确认将「${row.key}」重置为代码默认（${row.default_value || "空"}）？`,
      "重置为默认",
      { type: "warning" }
    );
  } catch {
    return;
  }
  try {
    const res = await deleteSystemConfig(row.key);
    if (res.success) {
      ElMessage.success("已重置为默认");
      await loadBusinessConfigs();
    } else {
      ElMessage.error("重置失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "请求失败");
  }
}

// 按 category 分组
const businessConfigsByCategory = computed(() => {
  const map = new Map<string, ConfigMeta[]>();
  for (const c of businessConfigs.value) {
    if (!map.has(c.category)) map.set(c.category, []);
    map.get(c.category)!.push(c);
  }
  return Array.from(map.entries()).map(([category, items]) => ({
    category,
    items
  }));
});

const systemConfig = ref({
  systemName: "文档管理系统",
  companyName: "",
  contactPerson: "",
  contactPhone: "",
  contactEmail: "",
  address: "",
  website: "",
  copyright: "",
  icpNumber: "",
  version: "1.0.0"
});

const configFile = ref({
  description: "测试环境配置",
  server: {
    host: "0.0.0.0",
    port: "8443",
    domain: "localhost",
    enable_ssl: false,
    ssl_cert: "./certs/public.pem",
    ssl_key: "./certs/private.pem"
  },
  jwt: { secret: "", access_expire: "24h", refresh_expire: "168h" },
  database: { path: "./bin/data/doc.db", mode: "" },
  cors: { allowed_origins: ["*"] },
  upload: { dir: "./bin/uploads/", max_size: 10485760 },
  log: { dir: "./bin/logs", level: "info", days_to_keep: 90, enabled: true },
  backup: {
    enabled: true,
    dir: "./bin/backups",
    days_to_keep: 90,
    database_enabled: true,
    upload_enabled: true
  },
});

const loadConfig = async () => {
  loading.value = true;
  try {
    const res: any = await http.request("get", "/api/system/config");
    if (res.success && res.data)
      systemConfig.value = { ...systemConfig.value, ...res.data };
  } catch (e) {
    console.error(e);
  }
  loading.value = false;
};

const loadConfigFile = async () => {
  loading.value = true;
  try {
    const res: any = await http.request("get", "/api/system/config-file");
    if (res.success && res.data) {
      configFile.value = { ...configFile.value, ...res.data };
      if (!configFile.value.server) {
        configFile.value.server = {
          host: "0.0.0.0",
          port: "8443",
          domain: "localhost",
          enable_ssl: false,
          ssl_cert: "./certs/public.pem",
          ssl_key: "./certs/private.pem"
        };
      }
      // pdf 字段已下线（PDF 字体配置后端不再读取，2026-07-06 round6）
    }
  } catch (e) {
    console.error(e);
  }
  loading.value = false;
};

// 2026-07-06 round6 精简：loadSystemFonts 函数已删除
//   /api/system/fonts 路由已在后端 round2 下线，调用会 404

const handleSaveSystemConfig = async () => {
  saving.value = true;
  try {
    const res: any = await http.request("post", "/api/system/config", {
      data: systemConfig.value
    });
    res.success
      ? ElMessage.success("保存成功")
      : ElMessage.error(res.message || "保存失败");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "保存失败");
  }
  saving.value = false;
};

const handleSaveConfigFile = async () => {
  // 2026-07-06 round6 精简：删除 encrypt_local + encrypt_passphrase 校验弹窗
  //   后端不再支持备份加密，配置文件中这两个字段会被忽略

  saving.value = true;
  try {
    const res: any = await http.request("post", "/api/system/config-file", {
      data: configFile.value
    });
    res.success
      ? ElMessage.success("配置文件保存成功，重启服务后生效")
      : ElMessage.error(res.message || "保存失败");
  } catch (e) {
    ElMessage.error("保存失败");
  }
  saving.value = false;
};

const generatingCert = ref(false);

const handleGenerateSSLCert = async () => {
  if (!configFile.value.server.domain) {
    ElMessage.warning("请先填写域名");
    return;
  }

  // 从配置中提取证书目录（使用ssl_cert或ssl_key的目录部分）
  const certPath = configFile.value.server.ssl_cert || "./certs/public.pem";
  const keyPath = configFile.value.server.ssl_key || "./certs/private.pem";
  const certDir = certPath.substring(0, certPath.lastIndexOf("/")) || "./certs";

  ElMessageBox.confirm(
    `确定要生成SSL证书吗？\n域名: ${configFile.value.server.domain}\n证书目录: ${certDir}\n有效期: 365天`,
    "生成SSL证书",
    { confirmButtonText: "确定生成", cancelButtonText: "取消", type: "info" }
  )
    .then(async () => {
      generatingCert.value = true;
      try {
        const res: any = await http.request(
          "post",
          "/api/system/generate-ssl-cert",
          {
            data: {
              cert_dir: certDir,
              common_name: configFile.value.server.domain,
              expired_days: 365
            }
          }
        );
        console.log("SSL证书响应:", res);
        if (res && res.success && res.data) {
          ElMessage.success("SSL证书生成成功！");
          const newCertPath =
            res.data.public_cert_path || res.data.publicCertPath;
          const newKeyPath =
            res.data.private_key_path || res.data.privateKeyPath;
          configFile.value.server.ssl_cert = newCertPath;
          configFile.value.server.ssl_key = newKeyPath;
          ElMessage.info(`证书已保存到: ${newCertPath}`);
        } else {
          ElMessage.error(res?.message || res?.error || "生成SSL证书失败");
        }
      } catch (e: any) {
        console.error("生成SSL证书失败:", e);
        ElMessage.error("生成SSL证书失败: " + (e?.message || "未知错误"));
      }
      generatingCert.value = false;
    })
    .catch(() => {});
};

const handleResetSystemConfig = () => {
  systemConfig.value = {
    systemName: "文档管理系统",
    companyName: "",
    contactPerson: "",
    contactPhone: "",
    contactEmail: "",
    address: "",
    website: "",
    copyright: "",
    icpNumber: "",
    version: "1.0.0"
  };
};

const handleResetConfigFile = async () => {
  await loadConfigFile();
};

const backingUp = ref(false);

const handleBackupNow = async () => {
  backingUp.value = true;
  try {
    const res: any = await http.request("post", "/api/system/backup-now");
    res.success
      ? ElMessage.success(res.message || "备份成功")
      : ElMessage.error(res.message || "备份失败");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "备份失败");
  }
  backingUp.value = false;
};

// 2026-07-06 round6 精简：copyMachineCode 函数已删除
//   license 字段已下线，无机器码可复制

const generateSecret = () => {
  const chars =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let secret = "";
  for (let i = 0; i < 32; i++) {
    secret += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  configFile.value.jwt.secret = secret;
  ElMessage.success("密钥已生成");
};

/**
 * 生成备份加密 passphrase（48 字符 = ~282 bit 熵，远超 AES-256 所需的 256 bit）
 *
 * 比 jwt secret 长度更长（48 vs 32），因为：
 *   - 主密钥不能丢失，强度需要更高
 *   - 48 字符 base62 提供约 282 bit 熵，暴力破解物理不可行
 *
 * 字符集与 jwt secret 一致（62 字符：a-z + A-Z + 0-9），便于用户识别。
 */
// 2026-07-06 round6 精简：generateBackupPassphrase 函数已删除
//   backup.encrypt_passphrase 字段已下线

const handleShutdown = async () => {
  try {
    await ElMessageBox.confirm(
      "确定要关闭服务器吗？关闭后服务将无法访问。",
      "关闭服务",
      {
        confirmButtonText: "确定关闭",
        cancelButtonText: "取消",
        type: "warning"
      }
    );
    await http.post("/api/system/shutdown");
    ElMessage.success("服务器正在关闭...");
  } catch (error) {
    if (error !== "cancel") {
      const msg = (error as any)?.response?.data?.message || (error as any)?.message || "关闭失败";
      ElMessage.error(msg);
    }
  }
};

// ========== 维护模式（第四阶段 P0）==========
const maintenanceEnabled = ref(false);
const maintenanceLoading = ref(false);

async function loadMaintenanceStatus() {
  try {
    const res: any = await http.request("get", "/api/system/maintenance");
    if (res.success && res.data) {
      maintenanceEnabled.value = !!res.data.maintenance_mode;
    }
  } catch (e) {
    console.error("[Maintenance] 加载状态失败", e);
  }
}

// 单按钮模式：按钮的 text + type 表达当前维护状态
const maintenanceButtonText = computed(() =>
  maintenanceEnabled.value ? "维护中… 点击关闭" : "开启维护模式"
);
const maintenanceButtonType = computed(() =>
  maintenanceEnabled.value ? "danger" : "warning"
);
const maintenanceHint = computed(() =>
  maintenanceEnabled.value
    ? "🔴 当前所有业务 API 已被拦截（仅 /api/health 与 /api/system/maintenance 可访问）"
    : "开启后所有业务 API 返回 503（仅 /api/health 与 /api/system/maintenance 可访问）"
);

async function handleToggleMaintenance() {
  // 点击按钮 = 切换到相反状态
  const targetValue = !maintenanceEnabled.value;

  // 弹确认框（用户取消时不动状态）
  try {
    const action = targetValue ? "开启" : "关闭";
    const warning = targetValue
      ? "开启维护模式后，所有业务 API 将返回 503，直到手动关闭。\n确认开启？"
      : "关闭维护模式，业务 API 将恢复正常访问。\n确认关闭？";
    await ElMessageBox.confirm(warning, `${action}维护模式`, {
      type: targetValue ? "warning" : "info",
      confirmButtonText: `确认${action}`,
      cancelButtonText: "取消"
    });
  } catch {
    return; // 用户取消
  }

  maintenanceLoading.value = true;
  try {
    const res: any = await http.request("post", "/api/system/maintenance", {
      data: { enabled: targetValue, reason: "管理员通过系统配置页面操作" }
    });
    if (res.success) {
      maintenanceEnabled.value = targetValue;
      ElMessage.success(`维护模式已${targetValue ? "开启" : "关闭"}`);
    } else {
      ElMessage.error(res.message || "操作失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "请求失败");
  }
  maintenanceLoading.value = false;
}

onMounted(() => {
  loadConfig();
  loadConfigFile();
  // 2026-07-06 round6 精简：删除 loadSystemFonts() 调用
  //   /api/system/fonts 路由已下线（round2 删），调会 404
  loadMaintenanceStatus();
  // 业务配置用 async IIFE：保证 onMounted 钩子返回前数据已就绪
  // （解决"切到 business tab 看到空表"问题）
  (async () => {
    await loadBusinessConfigs();
  })();
});
</script>

<template>
  <div class="system-config-container">
    <el-card>
      <template #header
        ><div class="card-header"><span>系统配置</span></div></template
      >
      <el-tabs v-model="activeTab" type="border-card" class="config-tabs">
        <el-tab-pane label="配置文件" name="file">
          <el-form
            v-loading="loading"
            :model="configFile"
            label-width="160px"
            style="max-width: 900px"
          >
            <el-divider content-position="left">服务器配置</el-divider>
            <el-form-item label="服务地址">
              <el-input
                v-model="configFile.server.host"
                placeholder="0.0.0.0 或 localhost"
              />
            </el-form-item>
            <el-form-item label="服务端口">
              <el-input v-model="configFile.server.port" placeholder="8443" />
            </el-form-item>
            <el-form-item label="域名">
              <el-input
                v-model="configFile.server.domain"
                placeholder="doc.example.com"
              />
            </el-form-item>
            <el-divider content-position="left">SSL/HTTPS配置</el-divider>
            <el-form-item label="启用HTTPS">
              <el-switch v-model="configFile.server.enable_ssl" />
            </el-form-item>
            <el-form-item v-if="configFile.server.enable_ssl" label="SSL证书">
              <el-input
                v-model="configFile.server.ssl_cert"
                placeholder="./certs/public.pem"
              />
            </el-form-item>
            <el-form-item v-if="configFile.server.enable_ssl" label="SSL私钥">
              <el-input
                v-model="configFile.server.ssl_key"
                placeholder="./certs/private.pem"
              />
            </el-form-item>
            <el-form-item v-if="configFile.server.enable_ssl">
              <el-alert type="info" :closable="false">
                启用HTTPS后，请确保证书文件存在。
                <br />
                <el-button
                  type="primary"
                  size="small"
                  :loading="generatingCert"
                  @click="handleGenerateSSLCert"
                >
                  一键生成SSL证书
                </el-button>
                <span style="margin-left: 10px; color: #666; font-size: 12px">
                  生成自签名证书，仅限内网使用。
                </span>
              </el-alert>
            </el-form-item>
            <el-divider content-position="left">CORS配置</el-divider>
            <el-form-item label="允许的源">
              <el-select
                v-model="configFile.cors.allowed_origins"
                multiple
                filterable
                allow-create
                default-first-option
                placeholder="输入域名后按回车添加"
                style="width: 100%"
              >
                <el-option
                  v-for="origin in configFile.cors.allowed_origins"
                  :key="origin"
                  :label="origin"
                  :value="origin"
                />
              </el-select>
            </el-form-item>
            <el-divider content-position="left">JWT配置</el-divider>
            <el-form-item label="密钥">
              <div style="display: flex; gap: 8px; align-items: center">
                <el-input
                  v-model="configFile.jwt.secret"
                  type="password"
                  show-password
                  style="flex: 1"
                />
                <el-button @click="generateSecret">生成密钥</el-button>
              </div>
            </el-form-item>
            <el-form-item label="访问令牌过期"
              ><el-input v-model="configFile.jwt.access_expire"
            /></el-form-item>
            <el-form-item label="刷新令牌过期"
              ><el-input v-model="configFile.jwt.refresh_expire"
            /></el-form-item>
            <el-divider content-position="left">数据库配置</el-divider>
            <el-form-item label="数据库路径"
              ><el-input v-model="configFile.database.path"
            /></el-form-item>
            <el-form-item label="运行模式">
              <el-select v-model="configFile.database.mode" placeholder="正常模式">
                <el-option label="正常（空/development/test/production）" value="" />
                <el-option
                  label="体验模式（experience，只读，拦截业务写请求）"
                  value="experience"
                >
                  <span style="float: left">体验模式</span>
                  <span
                    style="
                      float: right;
                      color: #e6a23c;
                      font-size: 12px;
                      font-weight: 600;
                    "
                    >experience</span
                  >
                </el-option>
              </el-select>
              <div class="form-tip">
                <el-tag v-if="configFile.database.mode === 'experience'" type="warning" size="small"
                  >体验模式：所有业务写操作会被后端拦截返回 423，前端顶部出现横幅</el-tag
                >
                <span v-else style="color: #909399">
                  环境变量 <code>DB_MODE</code> 非空时覆盖文件此项配置（env 优先）。
                </span>
              </div>
            </el-form-item>
            <el-divider content-position="left">上传配置</el-divider>
            <el-form-item label="上传目录"
              ><el-input v-model="configFile.upload.dir"
            /></el-form-item>
            <el-form-item label="非媒体上传最大尺寸 (MB)"
              ><el-input v-model.number="configFile.upload.max_size"
            /><div class="form-tip">模板/合同/印章等非媒体上传的限制。媒体中心按类型固定（图片 20MB / 音频 50MB / 视频 500MB），不受此项影响。</div>
            </el-form-item>
            <el-divider content-position="left">日志配置</el-divider>
            <el-form-item label="日志目录"
              ><el-input v-model="configFile.log.dir"
            /></el-form-item>
            <el-form-item label="日志级别">
              <el-select v-model="configFile.log.level">
                <el-option label="Debug" value="debug" />
                <el-option label="Info" value="info" />
                <el-option label="Warn" value="warn" />
                <el-option label="Error" value="error" />
              </el-select>
            </el-form-item>
            <el-form-item label="日志保留天数"
              ><el-input v-model.number="configFile.log.days_to_keep"
            /></el-form-item>
            <el-form-item label="启用日志"
              ><el-switch v-model="configFile.log.enabled"
            /></el-form-item>
            <el-divider content-position="left">备份配置</el-divider>
            <el-form-item label="启用备份"
              ><el-switch v-model="configFile.backup.enabled"
            /></el-form-item>
            <el-form-item label="备份目录"
              ><el-input v-model="configFile.backup.dir"
            /></el-form-item>
            <el-form-item label="备份保留天数"
              ><el-input v-model.number="configFile.backup.days_to_keep"
            /></el-form-item>
            <!-- 2026-07-06 round6 精简：删除「加密本地备份」+「加密密钥」配置块
                 后端不再支持备份加密（备份 zip 永远是明文） -->
            <el-form-item label="备份内容">
              <el-tag
                v-if="configFile.backup.database_enabled"
                type="success"
                style="margin-right: 8px"
                >数据库</el-tag
              >
              <el-tag v-if="configFile.backup.upload_enabled" type="success"
                >上传文件</el-tag
              >
              <span
                v-if="
                  !configFile.backup.database_enabled &&
                  !configFile.backup.upload_enabled
                "
                style="color: #999"
                >未选择任何备份内容</span
              >
            </el-form-item>
            <el-form-item>
              <el-button
                type="success"
                :loading="backingUp"
                @click="handleBackupNow"
                >立即备份</el-button
              >
              <span style="color: #999; margin-left: 10px"
                >数据库和上传文件将统一打包</span
              >
            </el-form-item>
            <!-- 2026-07-06 round6 精简：删除「许可证配置」块
                 后端 license 模块下线（无机器码、无 license_key、无到期校验） -->
            <!-- 2026-07-06 round6 精简：删除「PDF字体配置」块
                 后端 pdf.font 字段下线（PDF 生成用 pdfcpu 内置 Helvetica） -->
            <el-divider />
            <el-form-item>
              <el-button
                type="primary"
                :loading="saving"
                @click="handleSaveConfigFile"
                >保存配置文件</el-button
              >
              <el-button @click="handleResetConfigFile">重置</el-button>
              <el-button type="danger" @click="handleShutdown"
                >关闭服务</el-button
              >
              <el-button
                :type="maintenanceButtonType"
                :loading="maintenanceLoading"
                @click="handleToggleMaintenance"
              >
                {{ maintenanceButtonText }}
              </el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- ========== 业务配置 tab ========== -->
        <el-tab-pane label="业务配置" name="business">
          <div v-loading="businessLoading">
            <el-alert
              v-if="!hasPerms('system-config:update')"
              type="info"
              :closable="false"
              title="只读模式（仅管理员可编辑）"
              show-icon
              style="margin-bottom: 12px"
            />

            <div
              v-for="group in businessConfigsByCategory"
              :key="group.category"
              class="business-group"
            >
              <div class="group-title">
                <span class="group-bar" />
                <span class="group-text">{{ group.category }}</span>
                <span class="group-count">{{ group.items.length }} 项</span>
              </div>

              <el-table
                :data="group.items"
                border
                size="small"
                style="width: 100%"
              >
                <el-table-column prop="key" label="Key" width="200" />
                <el-table-column prop="value" label="当前值" min-width="180">
                  <template #default="{ row }">
                    <span :class="{ 'is-overridden': row.is_overridden }">
                      {{ row.value || "（空 = 用代码默认）" }}
                    </span>
                    <el-tag
                      v-if="row.is_overridden"
                      type="success"
                      size="small"
                      effect="plain"
                      style="margin-left: 8px"
                    >
                      已覆盖
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column
                  prop="default_value"
                  label="代码默认"
                  width="180"
                >
                  <template #default="{ row }">
                    <code class="default-code">{{
                      row.default_value || "（无）"
                    }}</code>
                  </template>
                </el-table-column>
                <el-table-column
                  prop="description"
                  label="说明"
                  min-width="220"
                >
                  <template #default="{ row }">
                    <el-tooltip :content="row.description" placement="top">
                      <span class="desc-text">{{ row.description }}</span>
                    </el-tooltip>
                  </template>
                </el-table-column>
                <el-table-column
                  v-perms="'system-config:update'"
                  label="操作"
                  width="200"
                  fixed="right"
                >
                  <template #default="{ row }">
                    <el-button
                      type="primary"
                      size="small"
                      link
                      @click="openBusinessEdit(row)"
                    >
                      编辑
                    </el-button>
                    <el-button
                      v-if="row.is_overridden"
                      type="warning"
                      size="small"
                      link
                      @click="resetBusinessToDefault(row)"
                    >
                      重置默认
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <el-empty
              v-if="!businessLoading && businessConfigsByCategory.length === 0"
              description="暂无业务配置"
            />
          </div>

          <!-- 业务配置编辑 dialog -->
          <el-dialog
            v-model="businessEditDialog.visible"
            title="编辑业务配置"
            width="540px"
            :close-on-click-modal="false"
          >
            <el-form label-width="100px">
              <el-form-item label="Key">
                <el-input v-model="businessEditDialog.key" disabled />
              </el-form-item>
              <el-form-item label="说明">
                <span class="form-desc">{{
                  businessEditDialog.description
                }}</span>
              </el-form-item>
              <el-form-item label="代码默认">
                <code>{{ businessEditDialog.defaultValue || "（无）" }}</code>
              </el-form-item>
              <el-form-item label="新值">
                <!-- bool 类型：开关 -->
                <el-switch
                  v-if="businessEditDialog.valueType === 'bool'"
                  v-model="businessEditDialog.value"
                  active-value="true"
                  inactive-value="false"
                  inline-prompt
                />
                <!-- enum 类型：下拉选择 -->
                <el-select
                  v-else-if="businessEditDialog.valueType === 'enum'"
                  v-model="businessEditDialog.value"
                  placeholder="请选择"
                  style="width: 100%"
                >
                  <el-option
                    v-for="opt in businessEditDialog.enumOptions"
                    :key="opt"
                    :label="opt"
                    :value="opt"
                  />
                </el-select>
                <!-- number 类型：数字输入 -->
                <el-input-number
                  v-else-if="businessEditDialog.valueType === 'number'"
                  v-model="businessEditDialog.value"
                  :min="0"
                  :step="1"
                  style="width: 100%"
                />
                <!-- text 类型（默认）：多行文本 -->
                <el-input
                  v-else
                  v-model="businessEditDialog.value"
                  type="textarea"
                  :rows="3"
                  placeholder="留空则用代码默认"
                />
              </el-form-item>
            </el-form>
            <template #footer>
              <el-button @click="businessEditDialog.visible = false"
                >取消</el-button
              >
              <el-button
                type="primary"
                :loading="businessSaving"
                @click="saveBusinessEdit"
              >
                保存
              </el-button>
            </template>
          </el-dialog>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<style scoped>
.system-config-container {
  padding: 20px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.config-tabs {
  margin-top: 20px;
}
:deep(.el-divider__text) {
  font-weight: 600;
  color: #303133;
}
.machine-code-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.copy-btn {
  margin-left: 8px;
}

/* 业务配置：分组 / 行 / 描述样式 */
.business-group {
  margin-bottom: 18px;
}
.group-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #303133;
}
.group-bar {
  display: inline-block;
  width: 3px;
  height: 14px;
  background: #c00000;
  border-radius: 2px;
}
.group-text {
  font-size: 14px;
}
.group-count {
  font-size: 12px;
  font-weight: 400;
  color: #909399;
}
.is-overridden {
  color: #67c23a;
  font-weight: 500;
}
.default-code {
  background: var(--el-fill-color-light, #f5f7fa);
  padding: 1px 6px;
  border-radius: 3px;
  font-family: monospace;
  font-size: 12px;
}
.desc-text {
  color: #606266;
  cursor: help;
  border-bottom: 1px dashed #c0c4cc;
}
.form-desc {
  color: #606266;
  font-size: 12px;
}
</style>
