//go:build windows

package adminapi

func diskUsage(path string) diskStats {
	return diskStats{Path: path}
}
