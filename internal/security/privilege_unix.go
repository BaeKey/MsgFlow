//go:build unix

package security

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// DropRootPrivileges 在 root 启动时主动降权到 nobody 用户，落实最小权限原则。
func DropRootPrivileges() error {
	if os.Geteuid() != 0 {
		return nil
	}

	targetUser, err := user.Lookup("nobody")
	if err != nil {
		return fmt.Errorf("lookup nobody user failed: %w", err)
	}

	uid, err := strconv.Atoi(targetUser.Uid)
	if err != nil {
		return fmt.Errorf("parse nobody uid failed: %w", err)
	}

	gid, err := strconv.Atoi(targetUser.Gid)
	if err != nil {
		return fmt.Errorf("parse nobody gid failed: %w", err)
	}

	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("clear supplementary groups failed: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid failed: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid failed: %w", err)
	}

	return nil
}
