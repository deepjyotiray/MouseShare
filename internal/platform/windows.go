//go:build windows

package platform

func newWindowsBridge() Bridge {
	return newStubBridge("windows")
}
