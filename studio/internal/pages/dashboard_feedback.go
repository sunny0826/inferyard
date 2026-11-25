package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

var knownErrorKeys = map[string]string{
	"candidate_description_invalid":       "err.candidate_description_invalid",
	"candidate_endpoint_mismatch":         "err.candidate_endpoint_mismatch",
	"service_identity_unavailable":        "err.service_identity_unavailable",
	"previous_scope_incomplete":           "err.previous_scope_incomplete",
	"candidate_binding_invalid":           "err.candidate_binding_invalid",
	"engine_working_directory_invalid":    "err.engine_isolation_invalid",
	"engine_environment_invalid":          "err.engine_isolation_invalid",
	"engine_process_isolation_failed":     "err.engine_isolation_invalid",
	"engine_candidate_mismatch":           "err.engine_candidate_mismatch",
	"model_discovery_limit":               "err.model_discovery_limit",
	"model_discovery_unreadable":          "err.model_discovery_unreadable",
	"service_closing":                     "err.service_closing",
	"run_active":                          "err.run_active",
	"engine_orphaned":                     "err.engine_orphaned",
	"engine_owner_invalid":                "err.engine_owner_invalid",
	"engine_owner_write_failed":           "err.engine_owner_write_failed",
	"engine_owner_remove_failed":          "err.engine_owner_remove_failed",
	"engine_stop_failed":                  "err.engine_stop_failed",
	"engine_stopping":                     "err.engine_stopping",
	"workspace_artifact_unreadable":       "err.workspace_artifact_unreadable",
	"workspace_busy":                      "err.workspace_busy",
	"engine_host_not_loopback":            "err.engine_host_not_loopback",
	"engine_credentials_forbidden":        "err.engine_credentials_forbidden",
	"engine_not_executable":               "err.engine_not_executable",
	"service_identity_unreadable":         "err.service_identity_unreadable",
	"unsupported_platform":                "err.unsupported_platform",
	"device_preflight_blocked":            "err.device_preflight_blocked",
	"dirty_service_requires_bound_recovery": "err.dirty_service_recovery_required",
	"native_dirty_requires_bound_recovery":  "err.dirty_service_recovery_required",
	"old_service_still_alive":               "err.old_service_still_alive",
	"previous_local_process_still_alive":    "err.old_service_still_alive",
	"recovery_requires_new_process":         "err.recovery_requires_new_process",
	"no_local_model":                      "err.no_local_model",
	"blocked":                             "err.blocked",
	"preflight_missing":                   "err.blocked",
	"incomplete":                          "err.incomplete",
	"run_incomplete":                      "err.incomplete",
	"single_run_resume_not_supported":     "err.single_run_resume_not_supported",
	"not_resumable":                       "err.not_resumable",
	"duration_resume_requires_new_window": "err.duration_resume_requires_new_window",
	"batch_plan_not_finished":             "err.batch_plan_not_finished",
	"cancelled":                           "err.cancelled",
	"output_name_exhausted":               "err.output_name_exhausted",
	"cli_blocked":                         "err.cli_blocked",
	"output_exists":                       "err.output_exists",
	"settings_incomplete":                 "err.settings_incomplete",
	"cli_unavailable":                     "err.cli_unavailable",
	"install_unsupported":                 "err.install_unsupported",
	"download_failed":                     "err.download_failed",
	"install_checksum_failed":             "err.install_checksum_failed",
	"install_unpack_failed":               "err.install_unpack_failed",
	"install_failed":                      "err.install_failed",
	"cli_upgrade_failed":                  "err.cli_upgrade_failed",
	"install_version_unknown":             "err.install_version_unknown",
	"ac_power_required":                   "err.ac_power_required",
	"chat_template_unavailable":           "err.chat_template_unavailable",
	"model_acquire_incomplete":            "err.model_acquire_incomplete",
	"engine_acquire_incomplete":           "err.engine_acquire_incomplete",
	"engine_checksum_mismatch":            "err.engine_checksum_mismatch",
	"token_env_missing":                   "err.token_env_missing",
	"auth_required":                       "err.auth_required",
	"rate_limited":                        "err.rate_limited",
	"upstream_http_error":                 "err.upstream_http_error",
	"unexpected_content_encoding":         "err.unexpected_content_encoding",
	"network_timeout":                     "err.network_timeout",
	"connection_error":                    "err.connection_error",
	"transfer_error":                      "err.transfer_error",
	"disk_full":                           "err.disk_full",
	"permission_denied":                   "err.permission_denied",
	"engine_unpack_failed":                "err.engine_unpack_failed",
	"engine_binary_missing":               "err.engine_binary_missing",
	"cli_outdated":                        "err.cli_outdated",
	"engine_check_error":                  "err.engine_check_error",
	"engine_check_invalid_response":       "err.engine_check_invalid_response",
	"invalid_library_manifest":            "err.invalid_library_manifest",
	"unsupported_engine_build_requires_adapter_validation": "err.unsupported_engine_build",
	"unsupported_runtime_profile":                          "err.unsupported_runtime_profile",
	"runtime_receipt_invalid":                              "err.runtime_receipt_invalid",
	"runtime_profile_mismatch":                             "err.runtime_profile_mismatch",
	"runtime_asset_mismatch":                               "err.runtime_asset_mismatch",
	"runtime_receipt_missing":                              "err.runtime_receipt_missing",
	"engine_version_query_failed":                          "err.engine_version_query_failed",
	"package_resource_invalid":                             "err.package_resource_invalid",
	"io_error":                                             "err.io_error",
	"invalid_input":                                        "err.invalid_input",
	"candidate_mismatch":                                   "err.candidate_mismatch",
	"engine_exited":                                        "err.engine_exited",
	"engine_start_timeout":                                 "err.engine_start_timeout",
	"engine_start_failed":                                  "err.engine_start_failed",
	"engine_port_in_use":                                   "err.engine_port_in_use",
	"service_discovery_invalid_response":                   "err.service_discovery_invalid_response",
	"service_discovery_error":                              "err.service_discovery_error",
	"service_discovery_unsupported_platform":               "err.service_discovery_unsupported_platform",
	"listener_enumeration_failed":                          "err.listener_enumeration_failed",
	"listener_table_incomplete":                            "err.listener_table_incomplete",
	"listener_ownership_incomplete":                        "err.listener_ownership_incomplete",
	"process_identity_unavailable":                         "err.process_identity_unavailable",
	"models_timeout":                                       "err.models_timeout",
	"models_unreachable":                                   "err.models_unreachable",
	"models_http_1xx":                                      "err.models_http_1xx",
	"models_http_3xx":                                      "err.models_http_3xx",
	"models_http_4xx":                                      "err.models_http_4xx",
	"models_http_5xx":                                      "err.models_http_5xx",
	"models_invalid_response":                              "err.models_invalid_response",
	"models_empty":                                         "err.models_empty",
	"models_response_too_large":                            "err.models_response_too_large",
	"health_timeout":                                       "err.health_timeout",
	"health_unreachable":                                   "err.health_unreachable",
	"startup_arguments_mismatch":                           "err.startup_arguments_mismatch",
	"service_process_unavailable":                          "err.service_process_unavailable",
	"binding_progress_changed":                             "err.binding_progress_changed",
	"report_unreadable":                                    "err.report_unreadable",
	"unsupported_report_format":                            "err.unsupported_report_format",
	"cli_error":                                            "err.cli_error",
	"invalid_request":                                      "err.invalid_request",
	"not_found":                                            "err.not_found",
	"conflict":                                             "err.conflict",
	"internal_error":                                       "err.internal_error",
	"error":                                                "err.error",
	"unknown":                                              "err.unknown",
}

func human(code string) string {
	if msg, ok := humanKnown(code); ok {
		return msg
	}
	return l10n.TRf("err.unknown_code", "code", code)
}

func humanKnown(code string) (string, bool) {
	if msg, ok := humanLocalEngineCode(code); ok {
		return msg, true
	}
	if key, ok := knownErrorKeys[code]; ok {
		return l10n.TR(key), true
	}
	return "", false
}

func (s *viewState) guideWork(fn func() (string, *core.ApiError)) {
	s.guideWorkAs("", fn)
}

func (s *viewState) guideWorkAs(action string, fn func() (string, *core.ApiError)) {
	if s.busy {
		return
	}
	s.workAs(action, func() (string, *core.ApiError) {
		message, e := fn()
		if e != nil {
			mapped := *e
			if mapped.Message == "" {
				mapped.Message = human(e.Code)
			}
			return "", &mapped
		}
		return message, nil
	})
}

func (s *viewState) feedback(c *ui.Context) {
	if s.failure != nil {
		if s.failurePage != "" && s.failurePage != s.selected {
			s.failure = nil
			s.failurePage = ""
		} else if !s.stepOwnsError(s.failure) {
			errorRecoveryBanner(c, s.failure, func() {
				if ghostButton(c, tr(c, "dashboard.feedback.close")).Clicked() {
					s.failure = nil
					s.failurePage = ""
				}
			})
		}
	}
	if s.failure == nil && s.refreshFailure != "" {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Size:    bannerCompact,
			Message: s.refreshFailure,
			Actions: func() {
				button(c, tr(c, "dashboard.feedback.retry")).OnClick(s.refreshAction)
				if ghostButton(c, tr(c, "dashboard.feedback.close")).Clicked() {
					s.refreshFailure = ""
				}
			},
		})
	}
	s.cliUpgradeBanner(c)
}

func (s *viewState) applyCLIUpgradeState(next core.CLIUpgradeState) {
	if next.State == "" {
		return
	}
	if s.cliUpgrade.State == "" || next.Seq >= s.cliUpgrade.Seq {
		s.cliUpgrade = next
		if next.State != core.CLIUpgradeFailed {
			s.cliUpgradeDismissedSeq = 0
		}
	}
}

func (s *viewState) cliAutoUpgradeActive() bool {
	return s.cliUpgrade.State == core.CLIUpgradeChecking || s.cliUpgrade.State == core.CLIUpgradeUpgrading
}

func (s *viewState) suppressOutdatedCLIError(e *core.ApiError) bool {
	return e != nil && e.Code == "cli_outdated" && (s.cliAutoUpgradeActive() || s.cliUpgradeErrorVisible())
}

func (s *viewState) cliUpgradeErrorVisible() bool {
	if s.cliUpgrade.State != core.CLIUpgradeFailed || s.cliUpgrade.Error == nil {
		return false
	}
	if s.cliUpgrade.Seq != 0 && s.cliUpgradeDismissedSeq == s.cliUpgrade.Seq {
		return false
	}
	if s.cliUpgrade.Seq == 0 && s.cliUpgradeDismissedSeq != 0 {
		return false
	}
	return true
}

func (s *viewState) cliUpgradeErrorMessage(c *ui.Context, e *core.ApiError) string {
	if e == nil {
		return ""
	}
	if e.Code == "cli_upgrade_failed" {
		return tr(c, "err.cli_upgrade_failed")
	}
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = human(e.Code)
	}
	return trf(c, "onboarding.cli.auto_upgrade_failed", "error", detail)
}

func (s *viewState) cliUpgradeBanner(c *ui.Context) {
	if s.cliUpgradeErrorVisible() {
		bannerErr := *s.cliUpgrade.Error
		bannerErr.Message = s.cliUpgradeErrorMessage(c, s.cliUpgrade.Error)
		ui.Column(c.Key("cli-upgrade-error-banner")).FillWidth().Children(func() {
			errorRecoveryBanner(c, &bannerErr, func() {
				upgrading := s.actionBusy("upgrade-cli") || s.cliAutoUpgradeActive()
				if primaryActionButton(c, tr(c, "dashboard.feedback.retry"), upgrading).Disabled(s.busy || upgrading).Clicked() {
					s.upgradeCLI()
				}
				if s.selected != "settings" {
					if button(c, tr(c, "onboarding.setup.open_settings")).Disabled(s.busy).Clicked() {
						s.openGeneralSettings()
					}
				}
				if ghostButton(c, tr(c, "dashboard.feedback.close")).Clicked() {
					if s.cliUpgrade.Seq != 0 {
						s.cliUpgradeDismissedSeq = s.cliUpgrade.Seq
					} else {
						s.cliUpgradeDismissedSeq = 1
					}
				}
			})
		})
		return
	}
	bound := s.service != nil && s.service.BoundPaths() != nil
	if !bound || !s.loaded || s.ready.CLI.Status != "outdated" || (s.selected == "dashboard" && s.detailID == "") {
		return
	}
	noticeBanner(c, noticeBannerOptions{
		Kind:    stateWarning,
		Size:    bannerCompact,
		Message: cliOutdatedSummary(s.ready.CLI.Version),
	})
}
