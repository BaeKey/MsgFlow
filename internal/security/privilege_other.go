//go:build !unix

package security

// DropRootPrivileges 在非 Unix 平台上无需处理，直接返回 nil。
func DropRootPrivileges() error {
	return nil
}
