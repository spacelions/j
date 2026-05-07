package resolver

// Interactive returns the effective interactive flag. Precedence:
// explicit (non-nil pointer) > default false.
//
// The stored bucket preference is removed per the HITL refactor.
// Every invocation must opt in via the --interactive flag; there
// is no longer a saved default.
func Interactive(explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	return false
}
