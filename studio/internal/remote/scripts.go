package remote

import (
	"fmt"
	"strings"
)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func packageSpec(version string) string {
	if version == "" || version == "latest" {
		return "inferyard"
	}
	return "inferyard==" + version
}

func unixJoinScript(session PairingSession, version, source, proxy string) (string, error) {
	if strings.ContainsAny(version+source+proxy, "\r\n\x00") {
		return "", fmt.Errorf("invalid pairing script metadata")
	}
	var script strings.Builder
	script.WriteString("#!/bin/sh\nset -eu\n")
	script.WriteString(unixUVBootstrap())
	if source != "" {
		if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			fmt.Fprintf(&script, "export UV_DEFAULT_INDEX=%s\n", shellQuote(source))
		} else {
			fmt.Fprintf(&script, "export INFERYARD_MODEL_SOURCE=%s\n", shellQuote(source))
		}
	}
	if proxy != "" {
		fmt.Fprintf(&script, "export HTTPS_PROXY=%s HTTP_PROXY=%s\n", shellQuote(proxy), shellQuote(proxy))
	}
	fmt.Fprintf(&script, "\"$UV\" tool install --python 3.15 --upgrade %s\n", shellQuote(packageSpec(version)))
	script.WriteString("for HUB in")
	for _, addr := range session.CandidateAddrs {
		fmt.Fprintf(&script, " %s", shellQuote(addr))
	}
	script.WriteString("; do\n")
	fmt.Fprintf(&script, "  \"$UV\" tool run --from %s inferyard join --hub \"$HUB\" --code %s --ca-sha256 %s && exit 0\n",
		shellQuote(packageSpec(version)), shellQuote(session.Code), shellQuote(session.CASha256))
	script.WriteString("done\necho 'Unable to reach an InferYard Hub address' >&2\nexit 1\n")
	return script.String(), nil
}

func windowsJoinScript(session PairingSession, version, source, proxy string) (string, error) {
	if strings.ContainsAny(version+source+proxy, "\r\n\x00") {
		return "", fmt.Errorf("invalid pairing script metadata")
	}
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	script.WriteString("$UvDir = if ($env:INFERYARD_UV_DIR) { $env:INFERYARD_UV_DIR } else { Join-Path $HOME '.local\\share\\inferyard\\bin' }\n")
	script.WriteString("$Uv = Join-Path $UvDir 'uv.exe'\n")
	script.WriteString("$MinUv = [version]'0.13.6'\n")
	script.WriteString("function Test-Uv($Path) { try { $Have = [version]((& $Path version | Select-Object -First 1) -replace '^uv\\s+','' -replace '\\s.*$',''); return $Have -ge $MinUv } catch { return $false } }\n")
	script.WriteString("if (-not (Test-Uv $Uv)) { $Cmd = Get-Command uv -ErrorAction SilentlyContinue; if ($Cmd -and (Test-Uv $Cmd.Source)) { $Uv = $Cmd.Source } else { $env:UV_INSTALL_DIR = $UvDir; irm https://astral.sh/uv/install.ps1 | iex; $Uv = Join-Path $UvDir 'uv.exe' } }\n")
	if source != "" {
		if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			fmt.Fprintf(&script, "$env:UV_DEFAULT_INDEX = %s\n", powerShellQuote(source))
		} else {
			fmt.Fprintf(&script, "$env:INFERYARD_MODEL_SOURCE = %s\n", powerShellQuote(source))
		}
	}
	if proxy != "" {
		fmt.Fprintf(&script, "$env:HTTPS_PROXY = %s\n$env:HTTP_PROXY = %s\n", powerShellQuote(proxy), powerShellQuote(proxy))
	}
	fmt.Fprintf(&script, "& $Uv tool install --python 3.15 --upgrade %s\n", powerShellQuote(packageSpec(version)))
	script.WriteString("$Hubs = @(")
	for index, addr := range session.CandidateAddrs {
		if index > 0 {
			script.WriteString(", ")
		}
		script.WriteString(powerShellQuote(addr))
	}
	script.WriteString(")\nforeach ($Hub in $Hubs) {\n")
	fmt.Fprintf(&script, "  & $Uv tool run --from %s inferyard join --hub $Hub --code %s --ca-sha256 %s\n  if ($LASTEXITCODE -eq 0) { exit 0 }\n",
		powerShellQuote(packageSpec(version)), powerShellQuote(session.Code), powerShellQuote(session.CASha256))
	script.WriteString("}\nthrow 'Unable to reach an InferYard Hub address'\n")
	return script.String(), nil
}

func unixUVBootstrap() string {
	return `UV_DIR="${INFERYARD_UV_DIR:-$HOME/.local/share/inferyard/bin}"
uv_version_ok() {
  [ -n "$1" ] || return 1
  HAVE=$("$1" version 2>/dev/null | awk 'NR == 1 { print $2 }') || return 1
  awk -v have="$HAVE" -v want="0.13.6" 'BEGIN {
    split(have, a, "."); split(want, b, ".")
    for (i = 1; i <= 3; i++) {
      if ((a[i] + 0) > (b[i] + 0)) exit 0
      if ((a[i] + 0) < (b[i] + 0)) exit 1
    }
    exit 0
  }'
}
install_uv() {
  curl --fail --silent --show-error https://astral.sh/uv/install.sh | env UV_INSTALL_DIR="$UV_DIR" INSTALLER_NO_MODIFY_PATH=1 sh
}
UV="$UV_DIR/uv"
if [ -x "$UV" ]; then
  uv_version_ok "$UV" || install_uv
elif command -v uv >/dev/null 2>&1; then
  UV=$(command -v uv)
  if ! uv_version_ok "$UV"; then install_uv; UV="$UV_DIR/uv"; fi
else
  install_uv
fi
`
}
