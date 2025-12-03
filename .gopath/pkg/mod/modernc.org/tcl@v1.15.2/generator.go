// Copyright 2020 The Tcl Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

//TODO enable threads
//TODO 8.6.11

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
	tarDir  = "tcl8.6.12"
	tarFile = tarName + ".tar.gz"
	tarName = tarDir + "-src"
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
		{"linux", "ppc64le"}: {},
		{"linux", "riscv64"}: {},
		{"linux", "s390x"}:   {},
		{"netbsd", "amd64"}:  {},
		{"openbsd", "amd64"}: {},
		{"openbsd", "arm64"}: {},
		{"windows", "386"}:   {},
		{"windows", "amd64"}: {},
	}
	loadConfig = ccgo.Env("GO_GENERATE_LOAD_CONFIG", "")
	saveConfig = ccgo.Env("GO_GENERATE_SAVE_CONFIG", "")
	tmpDir     = ccgo.Env("GO_GENERATE_TMPDIR", "")
)

func main() {
	fmt.Printf("Running on %s/%s.\n", runtime.GOOS, runtime.GOARCH)
	if _, ok := supported[supportedKey{goos, goarch}]; !ok {
		ccgo.Fatalf(true, "unsupported target: %s/%s", goos, goarch)
	}

	ccgo.MustMkdirs(true,
		"internal/tclsh",
		"internal/tcltest",
		"lib",
	)
	if tmpDir == "" {
		tmpDir = ccgo.MustTempDir(true, "", "go-generate-")
		defer os.RemoveAll(tmpDir)
	}
	srcDir := tmpDir + "/" + tarDir
	cdb, err := filepath.Abs(tmpDir + "/cdb.json")
	if err != nil {
		ccgo.Fatal(true, err)
	}

	haveCDB := true
	if _, err := os.Stat(cdb); err != nil {
		if !os.IsNotExist(err) {
			ccgo.Fatal(true, err)
		}

		haveCDB = false
	}

	if !haveCDB || saveConfig != "" {
		os.RemoveAll(srcDir)
		ccgo.MustUntarFile(true, tmpDir, tarFile, nil)
		ccgo.CopyDir(srcDir, filepath.Join("overlay", goos, goarch), nil)
		ccgo.MustCopyDir(true, "assets", srcDir+"/library", nil)
		ccgo.MustCopyDir(true, "testdata/tcl", srcDir+"/tests", nil)
		ccgo.MustCopyFile(true, "assets/tcltests/pkgIndex.tcl", "testdata/tcl/pkgIndex.tcl", nil)
		ccgo.MustCopyFile(true, "assets/tcltests/tcltests.tcl", "testdata/tcl/tcltests.tcl", nil)
	}

	cc, err := exec.LookPath(gcc)
	if err != nil {
		ccgo.Fatal(true, err)
	}

	os.Setenv("CC", cc)
	cfg := []string{
		"--disable-dll-unload",
		"--disable-load",
		"--disable-shared",
		// "--enable-symbols=mem", //TODO-
	}
	thr := "--disable-threads"
	switch fmt.Sprintf("%s/%s", goos, goarch) {
	case "linux/amd64":
		thr = "--enable-threads"
	}
	cfg = append(cfg, thr)
	platformDir := "/unix"
	var hide string
	switch goos {
	case "windows":
		hide = "TclWinCPUID"
	default:
		hide = "TclpCreateProcess"
	}
	lib := []string{
		"-D__printf__=printf",
		"-export-defines", "",
		"-export-enums", "",
		"-export-externs", "X",
		"-export-fields", "F",
		"-export-structs", "",
		"-export-typedefs", "",
		"-hide", hide,
		"-lmodernc.org/z/lib",
		"-o", filepath.Join("lib", fmt.Sprintf("tcl_%s_%s.go", goos, goarch)),
		"-pkgname", "tcl",
		"-replace-fd-zero", "__ccgo_fd_zero",
		"-replace-tcl-default-double-rounding", "__ccgo_tcl_default_double_rounding",
		"-replace-tcl-ieee-double-rounding", "__ccgo_tcl_ieee_double_rounding",
		"-trace-translation-units",
		"--load-config", loadConfig,
		"-save-config", saveConfig,
		cdb,
	}
	switch goos {
	case "windows":
		switch s := runtime.GOOS; s {
		case "linux":
			cfg = append(cfg, "--host=linux")
		default:
			ccgo.Fatalf(true, "unsupported cross compilation host: %s", s)
		}

		platformDir = "/win"
		lib = append(lib,
			"libtcl86.a",
			"libtclstub86.a",
		)
		cfg = append(cfg, "RC=x86_64-w64-mingw32-windres")
		cfg = append(cfg, "CFLAGS=-D_ATL_XP_TARGETING -DMP_FIXED_CUTOFFS -DMP_NO_STDINT -UHAVE_CAST_TO_UNION")
	case "darwin":
		cfg = append(cfg, "--enable-corefoundation=no")
		fallthrough
	case "linux", "freebsd", "netbsd":
		lib = append(lib,
			"libtcl8.6.a",
			"libtclstub8.6.a",
		)
	case "openbsd":
		lib = append(lib,
			"libtcl86.a",
			"libtclstub86.a",
		)
	}
	if !haveCDB {
		ccgo.MustInDir(true, srcDir+platformDir, func() error {
			ccgo.MustShell(true, "./configure", cfg...)
			switch fmt.Sprintf("%s/%s", goos, goarch) {
			case "darwin/amd64":
				ccgo.MustRun(true, "-compiledb", cdb, "make", "CFLAGS='-UHAVE_CPUID -UHAVE_COPYFILE'", "binaries", "tcltest")
			case "darwin/arm64":
				// This option currently causes trouble with gcc on darwin/arm64.
				// Ex: error: invalid variant 'BLEAH'
				// ccgo.MustShell(true, "sed", "-i", "", "s/ -mdynamic-no-pic//", "Makefile")
				ccgo.MustRun(true, "-compiledb", cdb, "gmake", "CFLAGS='-UHAVE_CPUID -UHAVE_COPYFILE'", "binaries", "tcltest")
			case "freebsd/amd64", "freebsd/386", "freebsd/arm", "netbsd/amd64":
				ccgo.MustRun(true, "-verbose-compiledb", "-compiledb", cdb, "gmake", "CFLAGS='-DNO_ISNAN -UHAVE_CPUID'", "binaries", "tcltest")
			case "openbsd/amd64", "openbsd/arm64":
				//TODO- ccgo.MustShell(true, "sed", "-i", `s/\\ __attribute__\\(\\(__visibility__\\(\\"hidden\\"\\)\\)\\)//`, "Makefile")
				//TODO- ccgo.MustShell(true, "sed", "-i", `s/-DTCL_SHLIB_EXT=\\"\\"//`, "Makefile")
				ccgo.MustRun(true, "-verbose-compiledb", "-compiledb", cdb, "gmake", "CFLAGS='-DNO_ISNAN -UHAVE_CPUID'", "binaries", "tcltest")
			case "linux/amd64":
				switch goarch {
				case "amd64":
					ccgo.MustShell(true, "sed", "-i", "s/ -DHAVE_PTHREAD_ATFORK=1//", "Makefile")
				}
				ccgo.MustRun(true, "-compiledb", cdb, "make", "CFLAGS=-UHAVE_CPUID -UHAVE_COPYFILE", "binaries", "tcltest")
			case
				"linux/386",
				"linux/arm",
				"linux/arm64",
				"linux/ppc64le",
				"linux/riscv64",
				"linux/s390x":

				ccgo.MustRun(true, "-compiledb", cdb, "make", "CFLAGS=-UHAVE_CPUID -UHAVE_COPYFILE", "binaries", "tcltest")
			case "windows/amd64", "windows/386":
				ccgo.MustRun(true, "-compiledb", cdb, "make", "binaries", "tcltest")
			}
			return nil
		})
	}

	ccgo.MustRun(true, lib...)
	switch goos {
	case "windows":
		ccgo.MustRun(true,
			"-DTCL_BROKEN_MAINARGS",
			"-export-defines", "",
			"-ignore-object", "tclsh.res.o",
			"-lmodernc.org/tcl/lib",
			"-nocapi",
			"-o", filepath.Join("internal", "tclsh", fmt.Sprintf("tclsh_%s_%s.go", goos, goarch)),
			"-pkgname", "tclsh",
			"-replace-fd-zero", "__ccgo_fd_zero",
			"-replace-tcl-default-double-rounding", "__ccgo_tcl_default_double_rounding",
			"-replace-tcl-ieee-double-rounding", "__ccgo_tcl_ieee_double_rounding",
			"-trace-translation-units",
			"--load-config", loadConfig,
			"-save-config", saveConfig,
			cdb, "tclsh86s.exe",
		)
		ccgo.MustRun(true,
			"-DTCL_BROKEN_MAINARGS",
			"-export-defines", "",
			"-ignore-object", "tclsh.res.o",
			"-lmodernc.org/tcl/lib",
			"-nocapi",
			"-o", filepath.Join("internal", "tcltest", fmt.Sprintf("tcltest_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			"--load-config", loadConfig,
			"-save-config", saveConfig,
			cdb, "tcltests.exe",
		)
	default:
		ccgo.MustRun(true,
			"-export-defines", "",
			"-lmodernc.org/tcl/lib",
			"-nocapi",
			"-o", filepath.Join("internal", "tclsh", fmt.Sprintf("tclsh_%s_%s.go", goos, goarch)),
			"-pkgname", "tclsh",
			"-replace-fd-zero", "__ccgo_fd_zero",
			"-replace-tcl-default-double-rounding", "__ccgo_tcl_default_double_rounding",
			"-replace-tcl-ieee-double-rounding", "__ccgo_tcl_ieee_double_rounding",
			"-trace-translation-units",
			"--load-config", loadConfig,
			"-save-config", saveConfig,
			cdb, "tclsh",
		)
		ccgo.MustRun(true,
			"-export-defines", "",
			"-lmodernc.org/tcl/lib",
			"-nocapi",
			"-o", filepath.Join("internal", "tcltest", fmt.Sprintf("tcltest_%s_%s.go", goos, goarch)),
			"-trace-translation-units",
			"--load-config", loadConfig,
			"-save-config", saveConfig,
			cdb,
			"tclAppInit.o#1",
			"tclTest.o",
			"tclTestObj.o",
			"tclTestProcBodyObj.o",
			"tclThreadTest.o",
			"tclUnixTest.o",
		)
	}
}
