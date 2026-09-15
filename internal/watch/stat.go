package watch

import "os"

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
