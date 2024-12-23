package utils

import (
	"os"
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
