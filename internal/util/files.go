package util

import (
	"os"
	"path/filepath"
	"strings"
)

type SourceFile struct {
	Path    string
	Content string
}

var supportedExt = map[string]bool{
	".go":   true,
	".md":   true,
	".yaml": true,
	".yml":  true,
	".json": true,
}

var skippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"vendor":       true,
}

func ScanSourceFiles(root string) ([]SourceFile, error) {
	var files []SourceFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, SourceFile{
			Path:    filepath.ToSlash(stripArchiveRoot(rel)),
			Content: string(data),
		})
		return nil
	})
	return files, err
}

func stripArchiveRoot(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= 1 {
		return path
	}
	return strings.Join(parts[1:], "/")
}
