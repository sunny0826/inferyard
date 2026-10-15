package core

// candidateEngineArgs resolves both binary and receipt from the same registry entry.
func (s *Service) candidateEngineArgs(args []string, name, platform string) ([]string, *ApiError) {
	for _, entry := range s.registry().engines {
		if entry.engine.Name != name {
			continue
		}
		flag, path, opposite := "--engine", entry.path, "--runtime-receipt"
		if platform == "windows" {
			flag, path, opposite = "--runtime-receipt", entry.receipt, "--engine"
			if path == "" {
				return nil, Err("runtime_receipt_missing")
			}
		}
		clean := make([]string, 0, len(args))
		for i := 0; i < len(args); i++ {
			if args[i] == opposite {
				i++
				continue
			}
			clean = append(clean, args[i])
		}
		return resourceArg(clean, flag, path), nil
	}
	return nil, Err("not_found")
}
