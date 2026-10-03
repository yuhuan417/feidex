package backend

// SelectedDriver follows a frontend's active backend. Stateless presentation
// services can be composed once without retaining a stale driver on switches.
type SelectedDriver struct{ Selected func() string }

func (d SelectedDriver) driver() Driver                   { return DriverForKind(d.Selected()) }
func (d SelectedDriver) Kind() string                     { return d.driver().Kind() }
func (d SelectedDriver) Capabilities() CapabilitySet      { return d.driver().Capabilities() }
func (d SelectedDriver) Runtime() RuntimeDriver           { return d.driver().Runtime() }
func (d SelectedDriver) Conversation() ConversationDriver { return d.driver().Conversation() }
func (d SelectedDriver) Permission() PermissionDriver     { return d.driver().Permission() }
