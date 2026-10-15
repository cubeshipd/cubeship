//go:build !unix

package dirarchive

import "os"

func owner(os.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
