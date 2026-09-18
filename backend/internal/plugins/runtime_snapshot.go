package plugins

// Caller holds mu again after an RPC. Results from a disabled, replaced or
// reconfigured generation must never be returned or committed as current.
func (m *Manager) checkRuntimeSnapshot(before Installation) error {
	if err := m.guard(m.db); err != nil {
		return err
	}
	now, err := m.load(before.ID)
	if err != nil {
		return err
	}
	if now.Generation != before.Generation || now.ConfigRevision != before.ConfigRevision || now.VersionID != before.VersionID || now.State != before.State || now.Enabled != before.Enabled {
		return ErrConflict
	}
	return nil
}
