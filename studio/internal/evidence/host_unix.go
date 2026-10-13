//go:build !windows

package evidence

func HostStatePath() (string, error) { return "/var/tmp/inferyard-host.state.json", nil }
