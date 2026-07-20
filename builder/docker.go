package builder

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MiloDevs/chibi-deploy/config"
	"github.com/moby/patternmatcher"
)

func CreateBuildContext(contextDir string) (io.Reader, error) {
	excludes, err := config.ParseDockerignore(contextDir)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		defer func() {
			tw.Close()
			pw.Close()
		}()

		err := filepath.Walk(contextDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			cleanRelPath := filepath.Join(contextDir, path)

			isMatch, err := patternmatcher.Matches(cleanRelPath, excludes)
			if err != nil {
				return err
			}

			if isMatch {
				return nil
			}

			fi, err := os.Lstat(path)

			var link string
			if fi.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}

			header, err := tar.FileInfoHeader(fi, link)
			if err != nil {
				return err
			}

			header.Name = filepath.ToSlash(cleanRelPath)
			if fi.IsDir() && !strings.HasSuffix(header.Name, "/") {
				header.Name += "/"
			}
			header.Format = tar.FormatPAX
			header.AccessTime = time.Time{}
			header.ChangeTime = time.Time{}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Only copy content for regular files, not symlinks or dirs
			if fi.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = io.Copy(tw, file)
				return err
			}
			return nil
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}
