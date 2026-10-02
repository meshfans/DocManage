//go:build linux

package machinecode

import "os"

// platformCandidates 返回 Linux 的硬件标识降级链。
//
// 按序取第一个「可读且非脏值」的来源。设计取舍见 machinecode.go 包注释。
//
// 关于 /sys/devices/virtual/dmi/id 与 /sys/class/dmi/id：
// 两者在多数发行版上是同一份数据的不同挂载点，保留两条是为了兼容
// 精简镜像（部分发行版不建 /sys/class/dmi 软链）。
//
// 关于「不用 MAC 地址」「不用 /etc/machine-id」：见包注释的排除清单。
// 特别提醒 /etc/machine-id 在虚拟机镜像里是**克隆复制的**，同一镜像拉起的
// 所有实例值完全相同，用它等于没绑定。
func platformCandidates() []candidate {
	return []candidate{
		{tier: 1, source: "/sys/devices/virtual/dmi/id/product_uuid", read: readFile("/sys/devices/virtual/dmi/id/product_uuid")},
		{tier: 2, source: "/sys/class/dmi/id/product_uuid", read: readFile("/sys/class/dmi/id/product_uuid")},
		{tier: 3, source: "/sys/class/dmi/id/board_serial", read: readFile("/sys/class/dmi/id/board_serial")},
		{tier: 4, source: "/sys/class/dmi/id/board_asset_tag", read: readFile("/sys/class/dmi/id/board_asset_tag")},
	}
}

// readFile 返回一个读取指定路径全部内容的 reader。
func readFile(path string) func() (string, error) {
	return func() (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
