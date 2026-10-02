package interaction

// Warning is an expected invalid or unavailable user choice.
type Warning struct{ Message string }

func (e Warning) Error() string { return e.Message }
