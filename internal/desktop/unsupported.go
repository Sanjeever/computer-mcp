//go:build !darwin && !windows

package desktop

func New() (Backend, error) {
	return nil, Fail("unsupported_platform", "macOS and Windows are supported")
}
