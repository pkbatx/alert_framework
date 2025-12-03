// Copyright 2021 The Zlib-Go Authors. All rights reserved.
// Use of the source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"modernc.org/ccgo/v3/lib"
)

const (
	tarDir  = "zlib-1.2.11"
	tarFile = tarName + ".tar.gz"
	tarName = tarDir
)

type supportedKey = struct{ os, arch string }

var (
	gcc       = ccgo.Env("GO_GENERATE_CC", ccgo.Env("CC", "gcc"))
	goarch    = ccgo.Env("TARGET_GOARCH", runtime.GOARCH)
	goos      = ccgo.Env("TARGET_GOOS", runtime.GOOS)
	supported = map[supportedKey]struct{}{
		{"darwin", "amd64"}:  {},
		{"darwin", "arm64"}:  {},
		{"freebsd", "386"}:   {},
		{"freebsd", "amd64"}: {},
		{"freebsd", "arm"}:   {},
		{"linux", "386"}:     {},
		{"linux", "amd64"}:   {},
		{"linux", "arm"}:     {},
		{"linux", "arm64"}:   {},
		{"linux", "riscv64"}: {},
		{"linux", "ppc64le"}: {},
		{"linux", "s390x"}:   {},
		{"netbsd", "amd64"}:  {},
		{"openbsd", "amd64"}: {},
		{"openbsd", "arm64"}: {},
		{"windows", "386"}:   {},
		{"windows", "amd64"}: {},
		{"windows", "arm64"}: {},
	}
	tmpDir = ccgo.Env("GO_GENERATE_TMPDIR", "")
)

func main() {
	fmt.Printf("Running on %s/%s.\n", runtime.GOOS, runtime.GOARCH)
	if _, ok := supported[supportedKey{goos, goarch}]; !ok {
		ccgo.Fatalf(true, "unsupported target: %s/%s", goos, goarch)
	}

	ccgo.MustMkdirs(true,
		"internal",
		"lib",
	)
	if tmpDir == "" {
		tmpDir = ccgo.MustTempDir(true, "", "go-generate-")
		defer os.RemoveAll(tmpDir)
	}
	srcDir := tmpDir + "/" + tarDir
	os.RemoveAll(srcDir)
	ccgo.MustUntarFile(true, tmpDir, tarFile, nil)
	cdb, err := filepath.Abs(tmpDir + "/cdb.json")
	if err != nil {
		ccgo.Fatal(true, err)
	}

	cc, err := exec.LookPath(gcc)
	if err != nil {
		ccgo.Fatal(true, err)
	}

	os.Setenv("CC", cc)
	if _, err := os.Stat(cdb); err != nil {
		if !os.IsNotExist(err) {
			ccgo.Fatal(true, err)
		}

		make := "make"
		ccgo.MustInDir(true, srcDir, func() error {
			switch goos {
			case "windows":
				ccgo.MustRun(true, "-compiledb", cdb, "make", "-fwin32/Makefile.gcc", "example.exe", "minigzip.exe")
			case "darwin", "freebsd", "netbsd", "openbsd":
				make = "gmake"
				fallthrough
			case "linux":
				ccgo.MustShell(true, "./configure", "--static")
				ccgo.MustRun(true, "-compiledb", cdb, make, "test64")
			}
			return nil
		})
	}
	switch goos {
	case "windows":
		ccgo.MustRun(true,
			"-export-defines", "",
			"-export-enums", "",
			"-export-externs", "X",
			"-export-fields", "F",
			"-export-structs", "",
			"-export-typedefs", "",
			"-o", filepath.Join("lib", fmt.Sprintf("z_%s_%s.go", goos, goarch)),
			"-pkgname", "z",
			"-trace-translation-units",
			cdb, "libz.a",
		)
		ccgo.MustRun(true,
			"-lmodernc.org/z/lib",
			"-o", filepath.Join("internal", fmt.Sprintf("minigzip_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			cdb, "minigzip.exe",
		)
		ccgo.MustRun(true,
			"-lmodernc.org/z/lib",
			"-o", filepath.Join("internal", fmt.Sprintf("example_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			cdb, "example.exe",
		)
	case "darwin", "linux", "freebsd", "netbsd", "openbsd":
		ccgo.MustRun(true,
			"-export-defines", "",
			"-export-enums", "",
			"-export-externs", "X",
			"-export-fields", "F",
			"-export-structs", "",
			"-export-typedefs", "",
			"-o", filepath.Join("lib", fmt.Sprintf("z_%s_%s.go", goos, goarch)),
			"-pkgname", "z",
			"-trace-translation-units",
			cdb, "libz.a",
		)
		ccgo.MustRun(true,
			"-lmodernc.org/z/lib",
			"-o", filepath.Join("internal", fmt.Sprintf("minigzip_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			cdb, "minigzip64",
		)
		ccgo.MustRun(true,
			"-lmodernc.org/z/lib",
			"-o", filepath.Join("internal", fmt.Sprintf("example_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			cdb, "example64",
		)
	}
}
