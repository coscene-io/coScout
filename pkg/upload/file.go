package upload

import (
	"path/filepath"
	"time"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/djherbis/times"
	log "github.com/sirupsen/logrus"
)

func ComputeUploadFiles(scanFolders []string, additionalFiles []string, startTime time.Time, endTime time.Time) (map[string]model.FileInfo, []string) {
	files := make(map[string]model.FileInfo)
	noPermissionFolders := make([]string, 0)

	for _, folder := range scanFolders {
		if !utils.CheckReadPath(folder) {
			log.Warnf("Path %s is not readable, skip!", folder)

			noPermissionFolders = append(noPermissionFolders, folder)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(folder)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to get all file paths in folder %s: %v", folder, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			log.Infof("file %s, mod time: %s", realPath, info.ModTime().String())
			//nolint: nestif // check file modification time
			if info.ModTime().After(startTime) && info.ModTime().Before(endTime) {
				filename, err := filepath.Rel(folder, realPath)
				if err != nil {
					log.Errorf("Failed to get relative path: %v", err)
					filename = filepath.Base(realPath)
				}

				files[realPath] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     realPath,
				}
			} else {
				stat, err := times.Stat(realPath)
				if err != nil {
					log.Errorf("Failed to get file times for %s: %v", realPath, err)
					continue
				}

				if stat.HasBirthTime() {
					log.Infof("File %s has birth time: %s", realPath, stat.BirthTime().String())
					if stat.BirthTime().After(startTime) && stat.BirthTime().Before(endTime) {
						filename, err := filepath.Rel(folder, realPath)
						if err != nil {
							log.Errorf("Failed to get relative path: %v", err)
							filename = filepath.Base(realPath)
						}

						files[realPath] = model.FileInfo{
							FileName: filename,
							Size:     info.Size(),
							Path:     realPath,
						}
					}
				}
			}
		}
	}

	for _, file := range additionalFiles {
		if !utils.CheckReadPath(file) {
			log.Warnf("Path %s is not readable, skip!", file)

			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(file)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		// Clean the file path to handle trailing slashes correctly
		cleanFile := filepath.Clean(realPath)
		parentFolder := filepath.Dir(cleanFile)
		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", realPath, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			filename, err := filepath.Rel(parentFolder, realPath)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			files[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
		}
	}

	return files, noPermissionFolders
}
