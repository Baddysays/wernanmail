//go:build unix

package adminapi

import "syscall"

func diskUsage(path string) diskStats {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskStats{Path: path}
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return diskStats{Path: path, TotalBytes: total, UsedBytes: used, FreeBytes: free, UsedPercent: pct}
}
