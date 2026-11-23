//go:build !darwin && !windows && !linux

package core

import "context"

func inspectPlatformInstallTarget(exePath string) updateInstallTarget {
	return updateInstallTarget{
		ExePath:       exePath,
		CanSelfUpdate: false,
		BlockCode:     "update_platform_unsupported",
		BlockMessage:  "当前平台暂不支持自动安装更新，请前往发布页下载。",
	}
}

func applyPlatformUpdate(context.Context, platformApplyPlan) error {
	return &updateCheckError{
		code:    "update_platform_unsupported",
		message: "当前平台暂不支持自动安装更新，请前往发布页下载。",
	}
}
