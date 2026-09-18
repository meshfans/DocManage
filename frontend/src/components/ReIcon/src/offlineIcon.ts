// 这里存放本地图标，在 src/layout/index.vue 文件中加载，避免在首启动加载
import { getSvgInfo } from "@pureadmin/utils";
import { addIcon } from "@iconify/vue/dist/offline";

// https://icon-sets.iconify.design/ep/?keyword=ep
import EpHomeFilled from "~icons/ep/home-filled?raw";

// https://icon-sets.iconify.design/ri/?keyword=ri
import RiSearchLine from "~icons/ri/search-line?raw";
import RiInformationLine from "~icons/ri/information-line?raw";
import RiFileTextLine from "~icons/ri/file-text-line?raw";
import RiUserSettingsLine from "~icons/ri/user-settings-line?raw";
import RiSettings3Line from "~icons/ri/settings-3-line?raw";
import RiSettings4Line from "~icons/ri/settings-4-line?raw";
import RiFileList3Line from "~icons/ri/file-list-3-line?raw";
import RiGitBranchLine from "~icons/ri/git-branch-line?raw";
import RiUserFollowLine from "~icons/ri/user-follow-line?raw";
import RiPagesLine from "~icons/ri/pages-line?raw";
import RiFlowChart from "~icons/ri/flow-chart?raw";
import RiTimeLine from "~icons/ri/time-line?raw";
import RiLockPasswordLine from "~icons/ri/lock-password-line?raw";
import RiMedalLine from "~icons/ri/medal-line?raw";
import RiCameraLine from "~icons/ri/camera-line?raw";
import RiVideoOnLine from "~icons/ri/video-on-line?raw";
import RiRefreshLine from "~icons/ri/refresh-line?raw";
import RiEarthLine from "~icons/ri/earth-line?raw";
import RiEyeLine from "~icons/ri/eye-line?raw";
import RiEyeOffLine from "~icons/ri/eye-off-line?raw";
import RiShieldKeyholeLine from "~icons/ri/shield-keyhole-line?raw";
import RiArchiveLine from "~icons/ri/archive-line?raw";
import RiTaskLine from "~icons/ri/task-line?raw";
import RiNotification3Line from "~icons/ri/notification-3-line?raw";
import RiVipCrownLine from "~icons/ri/vip-crown-line?raw";
import RiKey2Line from "~icons/ri/key-2-line?raw";
import RiUserLine from "~icons/ri/user-line?raw";
import RiBuildingLine from "~icons/ri/building-line?raw";
import RiVipCrown2Line from "~icons/ri/vip-crown-2-line?raw";
import RiGroupLine from "~icons/ri/group-line?raw";

// 2026-09-17：资料库菜单图标（顶级菜单 + 子菜单）
import RiFolderLine from "~icons/ri/folder-line?raw";
import RiImage2Line from "~icons/ri/image-2-line?raw";
// 关于我们页面图标
import RiMailLine from "~icons/ri/mail-line?raw";
import RiGlobalLine from "~icons/ri/global-line?raw";
import RiWechat2Line from "~icons/ri/wechat-2-line?raw";
// AI 配置页面图标
import RiBrainLine from "~icons/ri/brain-line?raw";

const icons = [
  // Element Plus Icon: https://github.com/element-plus/element-plus-icons
  ["ep/home-filled", EpHomeFilled],
  // Remix Icon: https://github.com/Remix-Design/RemixIcon
  ["ri/search-line", RiSearchLine],
  ["ri/information-line", RiInformationLine],
  ["ri/file-text-line", RiFileTextLine],
  ["ri/user-settings-line", RiUserSettingsLine],
  ["ri/settings-3-line", RiSettings3Line],
  ["ri/settings-4-line", RiSettings4Line],
  ["ri/file-list-3-line", RiFileList3Line],
  ["ri/git-branch-line", RiGitBranchLine],
  ["ri/user-follow-line", RiUserFollowLine],
  ["ri/pages-line", RiPagesLine],
  ["ri/flow-chart", RiFlowChart],
  ["ri/time-line", RiTimeLine],
  ["ri/lock-password-line", RiLockPasswordLine],
  ["ri/medal-line", RiMedalLine],
  ["ri/camera-line", RiCameraLine],
  ["ri/video-on-line", RiVideoOnLine],
  ["ri/refresh-line", RiRefreshLine],
  ["ri/earth-line", RiEarthLine],
  ["ri/eye-line", RiEyeLine],
  ["ri/eye-off-line", RiEyeOffLine],
  ["ri/shield-keyhole-line", RiShieldKeyholeLine],
  ["ri/archive-line", RiArchiveLine],
  ["ri/task-line", RiTaskLine],
  ["ri/notification-3-line", RiNotification3Line],
  // RBAC 新增
  ["ri/vip-crown-line", RiVipCrownLine],       // 角色管理
  ["ri/vip-crown-2-line", RiVipCrown2Line],     // 角色管理（备用）
  ["ri/key-2-line", RiKey2Line],                 // 权限管理
  ["ri/user-line", RiUserLine],                   // 个人客户
  ["ri/building-line", RiBuildingLine],           // 企业客户
  ["ri/group-line", RiGroupLine],                 // 客户列表（备用）
  // 2026-09-17：资料库菜单（顶级菜单 + 子菜单）
  ["ri/folder-line", RiFolderLine],               // 资料库顶级菜单
  ["ri/image-2-line", RiImage2Line],               // 媒体库（子菜单）
  // 关于我们页面图标
  ["ri/mail-line", RiMailLine],                   // 邮箱
  ["ri/global-line", RiGlobalLine],               // 网站
  ["ri/wechat-2-line", RiWechat2Line],           // 微信
  // AI 配置页面图标
  ["ri/brain-line", RiBrainLine]               // AI 大脑
];

// 本地菜单图标，后端在路由的 icon 中返回对应的图标字符串并且前端在此处使用 addIcon 添加即可渲染菜单图标
icons.forEach(([name, icon]) => {
  addIcon(name as string, getSvgInfo(icon as string));
});
