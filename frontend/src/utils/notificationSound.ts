/**
 * 新消息通知音：Web Audio API 机器合成 "叮咚"。
 *
 * 设计要点：
 *  - 0 资源依赖：浏览器原生 API，无需 .mp3 / Vite asset。
 *  - 0 依赖：纯原生，避免 howler.js / tone.js 增加 bundle 体积。
 *  - 单点权威：全应用仅此一个文件实现铃铛音，useMessageNotice 唯一调用方。
 *  - autoplay 友好：首次用户交互（click / keydown）后才能 resume context，
 *    失败静默不阻塞主流程。
 *  - 后端可控：DEFAULT_CONFIGS.notification_sound_enabled
 *    + 系统配置 > 业务配置 总开关（admin 可关）。
 *  - 防止爆音：每个 osc gain 设 0 → 0.18 → exp 衰减包络。
 *
 * 音色：
 *  - 第 1 声 "叮"  : sine 880→1100Hz 上扫，120ms
 *  - 第 2 声 "咚"  : triangle 1320→880Hz 下扫，200ms（错位 40ms）
 *  - 总时长 ~280ms，不打扰用户。
 */
import { getPublicSystemConfigs } from "@/api/system_config";

// 模块级状态（前端 SPA 单实例天然共享，无需 Pinia）
let enabled: boolean | null = null; // null = 未初始化（启动前）
let audioCtx: AudioContext | null = null; // 懒加载：首次播放才创建

/**
 * 从 system_config 拉取提示音开关。
 *   - DB 有值且非空 → 用 DB 值（admin 修改的覆盖）
 *   - DB 空 → 走 DEFAULT_CONFIGS 兜底（"true"）
 *   - 接口失败 → 默认开启（降级友好）
 *   - 后端 5s 进程内缓存：高频拉取不爆 DB。
 *
 * 通常在 main.ts 启动时调用一次；切换路由 / 重新登录可再次调用。
 */
export async function refreshNotificationSoundPref(): Promise<void> {
  try {
    const res = await getPublicSystemConfigs();
    const v = res.data?.notification_sound_enabled;
    // 兼容：DB 没值（DEFAULT_CONFIGS 兜底）→ 默认 true
    enabled = v === undefined ? true : String(v) === "true";
  } catch {
    // 网络/服务器失败：保守地保持"开启"，避免关掉服务
    enabled = true;
  }
}

/**
 * 播放新消息提示音（"叮咚"）。
 *
 * 行为：
 *  - enabled === false → 直接 return（admin 已关）
 *  - enabled === null  → 默认开启（启动前 pref 未拉取，按"开"处理）
 *  - autoplay policy 拦截 → 静默 catch，不打扰控制台
 *  - 老浏览器不支持 Web Audio → no-op
 *  - SSR / Node 环境 → no-op
 */
export function playNewMessageSound(): void {
  if (enabled === false) return;
  if (typeof window === "undefined") return;

  try {
    if (!audioCtx || audioCtx.state === "closed") {
      const Ctor =
        window.AudioContext ||
        (
          window as unknown as { webkitAudioContext?: typeof AudioContext }
        ).webkitAudioContext;
      if (!Ctor) return; // 极老浏览器兜底
      audioCtx = new Ctor();
    }

    // autoplay policy：context 可能 suspended 直到用户首次交互
    if (audioCtx.state === "suspended") {
      audioCtx.resume().catch(() => {
        /* 用户从未交互 → 永久 suspended，吞掉 */
      });
    }

    const now = audioCtx.currentTime;
    const peakVolume = 0.18; // 上限 0.2 避免高音压

    // 第 1 声 "叮"：sine 上扫，120ms
    scheduleTone(audioCtx, {
      startTime: now,
      duration: 0.12,
      freqStart: 880,
      freqEnd: 1100,
      peakVolume,
      waveType: "sine"
    });

    // 第 2 声 "咚"：triangle 下扫，错位 40ms 起，200ms
    scheduleTone(audioCtx, {
      startTime: now + 0.04,
      duration: 0.2,
      freqStart: 1320,
      freqEnd: 880,
      peakVolume: peakVolume * 0.85,
      waveType: "triangle"
    });
  } catch (e) {
    // dev 环境输出 warn，prod 静默（提示音是 UX 增强，非核心）
    if (import.meta.env.DEV) {
      console.warn("[notif-sound] synth failed:", e);
    }
  }
}

/**
 * 调度单个 oscillator + gain envelope + frequency ramp。
 */
interface ToneSpec {
  startTime: number;
  /** 秒 */
  duration: number;
  /** Hz */
  freqStart: number;
  /** Hz */
  freqEnd: number;
  /** 0~1 */
  peakVolume: number;
  waveType?: OscillatorType;
}

function scheduleTone(ctx: AudioContext, spec: ToneSpec): void {
  const {
    startTime,
    duration,
    freqStart,
    freqEnd,
    peakVolume,
    waveType = "sine"
  } = spec;

  const osc = ctx.createOscillator();
  osc.type = waveType;
  // 频率扫频（"叮"→"咚" 的滑音感）
  osc.frequency.setValueAtTime(freqStart, startTime);
  // exponentialRampToValueAtTime 不能接受 0（log），且 freqEnd 需 > 0
  osc.frequency.exponentialRampToValueAtTime(
    Math.max(1, freqEnd),
    startTime + duration
  );

  const gain = ctx.createGain();
  // 包络：attack 5ms + exp decay（避免 click / pop 爆音）
  gain.gain.setValueAtTime(0.0001, startTime); // 不能 0（exp）
  gain.gain.linearRampToValueAtTime(peakVolume, startTime + 0.005);
  gain.gain.exponentialRampToValueAtTime(0.0001, startTime + duration);

  osc.connect(gain).connect(ctx.destination);

  osc.start(startTime);
  osc.stop(startTime + duration + 0.01);
  // 显式释放节点（stop 后 disconnect，防止高频调用时节点积累）
  osc.addEventListener("ended", () => {
    osc.disconnect();
    gain.disconnect();
  }, { once: true });
}

/** 调试 / 测试用：读取当前开关。 */
export function isNotificationSoundEnabled(): boolean {
  return enabled !== false;
}
