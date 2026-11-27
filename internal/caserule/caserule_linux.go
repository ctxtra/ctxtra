package caserule

import (
	"os"

	"golang.org/x/sys/unix"
)

const (
	// fsCasefoldFl is FS_CASEFOLD_FL of <linux/fs.h>: the folder ignores
	// case.
	fsCasefoldFl = 0x40000000
	// ntfsMagic is the older NTFS driver's, which ignores case by default.
	// The newer ntfs3 driver doesn't unless mounted with nocase, so it stays
	// unknown.
	ntfsMagic = 0x5346544e
)

// Filesystems whose names ignore case, as mounted by default.
var insensitive = map[int64]bool{
	unix.MSDOS_SUPER_MAGIC: true,
	unix.EXFAT_SUPER_MAGIC: true,
	ntfsMagic:              true,
	unix.CIFS_SUPER_MAGIC:  true,
	unix.SMB2_SUPER_MAGIC:  true,
}

// Filesystems where a folder ignores case only with the casefold flag.
var casefold = map[int64]bool{
	unix.EXT4_SUPER_MAGIC: true,
	unix.F2FS_SUPER_MAGIC: true,
	unix.TMPFS_MAGIC:      true,
}

// Filesystems that never ignore case. XFS's ASCII-only option is deprecated
// in the kernel.
var sensitive = map[int64]bool{
	unix.BTRFS_SUPER_MAGIC: true,
	unix.XFS_SUPER_MAGIC:   true,
}

func of(dir string) (Rule, error) {
	f, err := os.Open(dir)
	if err != nil {
		return Unknown, err
	}
	defer f.Close()
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return Unknown, nil
	}
	t := int64(st.Type)
	switch {
	case insensitive[t]:
		return Insensitive, nil
	case sensitive[t]:
		return Sensitive, nil
	case casefold[t]:
		flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
		switch {
		case err != nil:
			return Unknown, nil
		case flags&fsCasefoldFl != 0:
			return Insensitive, nil
		}
		return Sensitive, nil
	}
	return Unknown, nil
}
