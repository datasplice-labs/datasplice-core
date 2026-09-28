package pipeline

// destinationDescriber is implemented by packages that can say where their
// data comes from or goes to, for `plan`. It gets the step's interpolated
// `with:` and must not touch the network or the filesystem.
type destinationDescriber interface {
	Destination(with map[string]any) string
}
