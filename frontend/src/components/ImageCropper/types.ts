/**
 * ImageCropper 类型定义
 */

// 输出格式
export type OutputFormat = 'webp' | 'jpeg' | 'png';

// 组件配置选项
export interface CropperOptions {
  // 输出配置
  outputWidth?: number;
  outputHeight?: number;
  outputFormat?: OutputFormat;
  quality?: number;

  // 限制
  maxSize?: number;
  accept?: string;
  aspectRatio?: number;

  // 样式定制
  backgroundColor?: string;
  maskColor?: string;
  borderColor?: string;
  guideColor?: string;

  // UI 配置
  title?: string;
  previewSize?: number;
  showGuideGrid?: boolean;
  roundedCrop?: boolean;

  // 行为
  minScale?: number;
  maxScale?: number;
  boundToCrop?: boolean;
}

// 完整类型（所有属性都有值）
export interface FullCropperOptions {
  outputWidth: number;
  outputHeight: number;
  outputFormat: OutputFormat;
  quality: number;
  maxSize: number;
  accept: string;
  aspectRatio: number;
  backgroundColor: string;
  maskColor: string;
  borderColor: string;
  guideColor: string;
  title: string;
  previewSize: number;
  showGuideGrid: boolean;
  roundedCrop: boolean;
  minScale: number;
  maxScale: number;
  boundToCrop: boolean;
}

// 合并后的完整配置
export const fullDefaultOptions: FullCropperOptions = {
  outputWidth: 300,
  outputHeight: 300,
  outputFormat: 'webp',
  quality: 0.85,
  maxSize: 5 * 1024 * 1024,
  accept: 'image/*',
  aspectRatio: 0,
  backgroundColor: '#1f2329',
  maskColor: 'rgba(0, 0, 0, 0.5)',
  borderColor: '#409eff',
  guideColor: 'rgba(255, 255, 255, 0.4)',
  title: '裁剪图片',
  previewSize: 380,
  showGuideGrid: true,
  roundedCrop: false,
  minScale: 0.1,
  maxScale: 5,
  boundToCrop: true,
};

// 获取完整配置（合并默认配置）
export function getFullOptions(options?: Partial<CropperOptions>): FullCropperOptions {
  return {
    ...fullDefaultOptions,
    ...options,
  };
}
