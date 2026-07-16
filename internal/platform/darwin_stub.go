//go:build !darwin

package platform

func newDarwinBridge() Bridge {
	return newStubBridge("macos")
}
