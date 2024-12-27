package utils

import (
	"os"
	"path/filepath"
)

func CheckReadPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Mode().Perm()&0444 == 0444 {
		return true
	}
	return false
}

func DeleteDir(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return true
	}

	err := os.RemoveAll(dir)
	if err != nil {
		return false
	}
	return true
}

func GetParentFolder(path string) string {
	return filepath.Dir(path)
}
