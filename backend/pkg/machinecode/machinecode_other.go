//go:build !linux && !windows

package machinecode

// platformCandidates 在非 Linux/Windows 平台（darwin / freebsd 等）返回空链。
//
// 结果：Compute() 返回 ErrUnavailable，调用方在 strict 模式下拒绝启动。
//
// 这是有意的 fail-closed 行为：宁可拒绝启动，也不给未绑定的授权开后门。
// 若未来需要支持 macOS，应新增 machinecode_darwin.go（用 IOPlatformUUID）。
func platformCandidates() []candidate {
	return nil
}
