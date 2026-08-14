//go:build darwin || linux

package main

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

var errUnstableReleaseArtifact = errors.New("release artifact is not a stable regular file")

func openReleaseArtifact(path string) (io.ReadCloser, error) {
	return openReleaseArtifactWith(path, openArtifactNoFollow)
}

func openReleaseArtifactWith(path string, open func(string) (*os.File, error)) (io.ReadCloser, error) {
	if path == "" || open == nil {
		return nil, errUnstableReleaseArtifact
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errUnstableReleaseArtifact
	}
	file, err := open(path)
	if err != nil {
		return nil, errUnstableReleaseArtifact
	}
	if file == nil {
		return nil, errUnstableReleaseArtifact
	}
	closeOnError := func() (io.ReadCloser, error) {
		_ = file.Close()
		return nil, errUnstableReleaseArtifact
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return closeOnError()
	}
	after, err := os.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(opened, after) {
		return closeOnError()
	}
	return file, nil
}

func openArtifactNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errUnstableReleaseArtifact
	}
	return file, nil
}
