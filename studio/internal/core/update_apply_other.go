//go:build !darwin && !windows && !linux

package core

import (
	"context"
	"inferyard/studio/internal/l10n"
)

func inspectPlatformInstallTarget(exePath string) updateInstallTarget {
	return updateInstallTarget{
		ExePath:       exePath,
		CanSelfUpdate: false,
		BlockCode:     "update_platform_unsupported",
		BlockMessage:  l10n.TR("core.update.err.platform_unsupported"),
	}
}

func applyPlatformUpdate(context.Context, platformApplyPlan) error {
	return &updateCheckError{
		code:    "update_platform_unsupported",
		message: l10n.TR("core.update.err.platform_unsupported"),
	}
}
