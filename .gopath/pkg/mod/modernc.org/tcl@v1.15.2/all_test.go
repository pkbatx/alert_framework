// Copyright 2020 The Tcl Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tcl // import "modernc.org/tcl"

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"

	"modernc.org/ccgo/v3/lib"
	"modernc.org/libc"
	"modernc.org/tcl/lib"
)

func caller(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(2)
	fmt.Fprintf(os.Stderr, "# caller: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	_, fn, fl, _ = runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# \tcallee: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func dbg(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# dbg %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

var traceLevel int32

func trace() func() {
	n := atomic.AddInt32(&traceLevel, 1)
	pc, file, line, _ := runtime.Caller(1)
	s := strings.Repeat("· ", int(n)-1)
	fn := runtime.FuncForPC(pc)
	fmt.Fprintf(os.Stderr, "%s# trace %s:%d:%s: in\n", s, path.Base(file), line, fn.Name())
	os.Stderr.Sync()
	return func() {
		atomic.AddInt32(&traceLevel, -1)
		fmt.Fprintf(os.Stderr, "%s# trace %s:%d:%s: out\n", s, path.Base(file), line, fn.Name())
		os.Stderr.Sync()
	}
}

func TODO(...interface{}) string { //TODOOK
	_, fn, fl, _ := runtime.Caller(1)
	return fmt.Sprintf("# TODO: %s:%d:\n", path.Base(fn), fl) //TODOOK
}

func stack() string { return string(debug.Stack()) }

func use(...interface{}) {}

func init() {
	use(caller, dbg, TODO, trace, stack) //TODOOK
}

// ============================================================================

var (
	oDebug      = flag.String("debug", "", "argument of -debug passed to the Tcl test suite: https://www.tcl.tk/man/tcl8.4/TclCmd/tcltest.htm#M91")
	oFile       = flag.String("file", "", "argument of -file passed to the Tcl test suite: https://www.tcl.tk/man/tcl8.4/TclCmd/tcltest.htm#M110")
	oMatch      = flag.String("match", "", "argument of -match passed to the Tcl test suite: https://www.tcl.tk/man/tcl8.4/TclCmd/tcltest.htm#114")
	oSingleProc = flag.Bool("singleproc", false, "argument of -singleproc passed to the Tcl test suite: https://www.tcl.tk/man/tcl8.4/TclCmd/tcltest.htm#M90")
	oVerbose    = flag.String("verbose", "", "argument of -verbose passed to the Tcl test suite: https://www.tcl.tk/man/tcl8.4/TclCmd/tcltest.htm#M96")
	oXTags      = flag.String("xtags", "", "passed to go build of tcltest in TestTclTest")
)

func TestMain(m *testing.M) {
	fmt.Printf("test binary compiled for %s/%s\n", runtime.GOOS, runtime.GOARCH)
	flag.Parse()
	libc.MemAuditStart()
	os.Exit(m.Run())
}

func TestTclTest(t *testing.T) {
	skip := []string{}
	var notFile []string
	switch fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH) {
	case "darwin/amd64":
		notFile = []string{ //TODO
			"fCmd.test",
			"macOSXFCmd.test",
			"socket.test",
		}
		skip = []string{ //TODO
			"oo-15.10",
			"oo-15.4",
			"oo-15.5",
			"oo-35.6",
			"cmdAH-7.1",
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"cmdMZ-6.5a",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"safe-16.2",
			"safe-16.7",
			"safe-16.8",
			"tcltest-9.5",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "darwin/arm64":
		notFile = []string{ //TODO
			"fCmd.test",
			"socket.test",
		}
		skip = []string{
			//TODO
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"cmdMZ-6.5a",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"oo-15.10",
			"oo-15.4",
			"oo-15.5",
			"oo-35.6",
			"safe-16.2",
			"safe-16.7",
			"safe-16.8",
			"tcltest-9.5",
			"unixFCmd-2.4",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "freebsd/arm64":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"binary-40.3",
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"unixFCmd-20.1",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "freebsd/amd64":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"tcltest-9.5",
			"unixFCmd-20.1",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "openbsd/amd64":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"Tcl_Main-1.3",
			"Tcl_Main-1.4",
			"Tcl_Main-1.5",
			"Tcl_Main-1.6",
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"fCmd-9.4.b",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"unixFCmd-2.4",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "openbsd/arm64":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"Tcl_Main-1.3",
			"Tcl_Main-1.4",
			"Tcl_Main-1.5",
			"Tcl_Main-1.6",
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"exit-1.1",
			"fCmd-9.4.b",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"unixFCmd-2.4",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "freebsd/arm":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"binary-40.3",
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "freebsd/386":
		notFile = []string{ //TODO
			"socket.test",
		}
		skip = []string{ //TODO
			"chan-16.9",
			"chan-io-28.7",
			"chan-io-29.34",
			"chan-io-29.35",
			"chan-io-39.18",
			"chan-io-39.19",
			"chan-io-39.20",
			"chan-io-39.21",
			"chan-io-39.23",
			"chan-io-39.24",
			"chan-io-51.1",
			"chan-io-53.10",
			"chan-io-53.5",
			"chan-io-54.1",
			"chan-io-54.2",
			"chan-io-57.1",
			"chan-io-57.2",
			"event-11.5",
			"http11-1.0",
			"http11-1.1",
			"http11-1.10",
			"http11-1.11",
			"http11-1.12",
			"http11-1.13",
			"http11-1.2",
			"http11-1.3",
			"http11-1.4",
			"http11-1.5",
			"http11-1.6",
			"http11-1.7",
			"http11-1.8",
			"http11-1.9",
			"http11-2.0",
			"http11-2.1",
			"http11-2.10",
			"http11-2.11",
			"http11-2.12",
			"http11-2.2",
			"http11-2.3",
			"http11-2.4",
			"http11-2.4.1",
			"http11-2.5",
			"http11-2.6",
			"http11-2.7",
			"http11-2.8",
			"http11-2.9",
			"http11-3.0",
			"http11-3.1",
			"http11-3.2",
			"http11-3.3",
			"http11-3.4",
			"http11-3.5",
			"http11-3.6",
			"http11-3.7",
			"http11-3.8",
			"http11-3.9",
			"http11-4.0",
			"http11-4.1",
			"http11-4.2",
			"http11-4.3",
			"io-29.34",
			"io-29.35",
			"io-39.18",
			"io-39.19",
			"io-39.20",
			"io-39.21",
			"io-39.23",
			"io-39.24",
			"io-51.1",
			"io-53.10",
			"io-53.5",
			"io-54.1",
			"io-54.2",
			"io-57.1",
			"io-57.2",
			"iocmd-8.15.1",
			"iocmd-8.16",
			"unixFCmd-20.1",
			"unixInit-1.2",
			"zlib-10.0",
			"zlib-10.1",
			"zlib-10.2",
			"zlib-8.3",
			"zlib-9.10",
			"zlib-9.11",
			"zlib-9.2",
			"zlib-9.3",
			"zlib-9.4",
			"zlib-9.5",
			"zlib-9.6",
			"zlib-9.7",
			"zlib-9.8",
			"zlib-9.9",
		}
	case "linux/amd64":
		skip = []string{"cmdIL-5.7"}
	case "linux/ppc64le":
		skip = []string{
			"encoding-16.3",    //TODO
			"socket_inet-2.13", //TODO
		}
	case "netbsd/amd64":
		notFile = []string{ //TODO
			"chan.test",
			"chanio.test",
			"clock.test",
			"cmdAH.test",
			"event.test",
			"exec.test",
			"fCmd.test",
			"fileName.test",
			"fileSystem.test",
			"http11.test",
			"info.test",
			"io.test",
			"ioCmd.test",
			"main.test",
			"socket.test",
			"tcltest.test",
			"unixFCmd.test",
			"unixInit.test",
			"zlib.test",
		}
	case "windows/amd64":
		notFile = []string{
			//TODO
			"env.test",
			"exec.test",
			"http.test",
			"http11.test",
			"httpold.test",
			"io.test",
			"socket.test",
		}
		skip = []string{
			//TODO
			"Tcl_Main-4\\.*",
			"Tcl_Main-5\\.*",
			"chan-16.9",
			"chan-io-2*",
			"chan-io-3*",
			"chan-io-4*",
			"chan-io-5*",
			"clock-38.2",
			"clock-40.1",
			"clock-42.1",
			"clock-49.2",
			"cmdAH-16*",
			"cmdAH-17*",
			"cmdAH-19*",
			"cmdAH-2.5",
			"cmdAH-20*",
			"cmdAH-24*",
			"cmdAH-25*",
			"event-11*",
			"event-12.4",
			"event-7.5",
			"fCmd-10\\.*",
			"fCmd-16.6",
			"fCmd-27\\.*",
			"fCmd-29\\.*",
			"fCmd-3\\.*",
			"fCmd-4\\.*",
			"fCmd-5\\.*",
			"fCmd-6.17",
			"fCmd-9\\.*",
			"filename-10.7",
			"filename-11\\.*",
			"filename-15\\.*",
			"filesystem-1\\.*",
			"filesystem-6\\.*",
			"filesystem-7\\.*",
			"filesystem-9\\.*",
			"info-16\\.*",
			"interp-34\\.*",
			"iocmd-31\\.*",
			"iocmd-32\\.*",
			"iocmd-8\\.*",
			"package-15\\.*",
			"platform-3.1",
			"regexp-22\\.*",
			"safe-8.5",
			"safe-8.6",
			"safe-8.7",
			"safe-stock-7.4",
			"tcltest-5\\.*",
			"tcltest-8\\.*",
			"tcltest-9.5",
			"timer-11\\.*",
			"timer-3\\.*",
			"timer-6\\.*",
			"timer-9\\.*",
			"winFCmd-1\\.*",
			"winFCmd-2\\.*",
			"winFCmd-3\\.*",
			"winFCmd-6\\.*",
			"winFCmd-7\\.*",
			"winFCmd-8\\.*",
			"winFCmd-9\\.*",
			"winNotify-3\\.*",
			"winTime-1.2",
			"winTime-2.1",
			"winpipe-4\\.*",
			"zlib-10\\.*",
			"zlib-8\\.*",
			"zlib-9\\.*",
		}
	case "windows/arm64":
		notFile = []string{
			//TODO
			"env.test",
			"exec.test",
			"http.test",
			"http11.test",
			"httpold.test",
			"io.test",
			"socket.test",
		}
		skip = []string{
			//TODO
			"Tcl_Main-4\\.*",
			"Tcl_Main-5\\.*",
			"chan-16.9",
			"chan-io-2*",
			"chan-io-3*",
			"chan-io-4*",
			"chan-io-5*",
			"clock-38.2",
			"clock-40.1",
			"clock-42.1",
			"clock-49.2",
			"cmdAH-16*",
			"cmdAH-17*",
			"cmdAH-19*",
			"cmdAH-2.5",
			"cmdAH-20*",
			"cmdAH-24*",
			"cmdAH-25*",
			"event-11*",
			"event-12.4",
			"event-7.5",
			"fCmd-10\\.*",
			"fCmd-16.6",
			"fCmd-27\\.*",
			"fCmd-29\\.*",
			"fCmd-3\\.*",
			"fCmd-4\\.*",
			"fCmd-5\\.*",
			"fCmd-6.17",
			"fCmd-9\\.*",
			"filename-10.7",
			"filename-11\\.*",
			"filename-15\\.*",
			"filesystem-1\\.*",
			"filesystem-6\\.*",
			"filesystem-7\\.*",
			"filesystem-9\\.*",
			"info-16\\.*",
			"interp-34\\.*",
			"iocmd-31\\.*",
			"iocmd-32\\.*",
			"iocmd-8\\.*",
			"package-15\\.*",
			"platform-3.1",
			"regexp-22\\.*",
			"safe-8.5",
			"safe-8.6",
			"safe-8.7",
			"safe-stock-7.4",
			"tcltest-5\\.*",
			"tcltest-8\\.*",
			"tcltest-9.5",
			"timer-11\\.*",
			"timer-3\\.*",
			"timer-6\\.*",
			"timer-9\\.*",
			"winFCmd-1\\.*",
			"winFCmd-2\\.*",
			"winFCmd-3\\.*",
			"winFCmd-6\\.*",
			"winFCmd-7\\.*",
			"winFCmd-8\\.*",
			"winFCmd-9\\.*",
			"winNotify-3\\.*",
			"winTime-1.2",
			"winTime-2.1",
			"winpipe-4\\.*",
			"zlib-10\\.*",
			"zlib-8\\.*",
			"zlib-9\\.*",
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	pth, err := filepath.Abs(wd)
	if err != nil {
		t.Fatal(err)
	}

	g := newGolden(t, filepath.Join(pth, "testdata", fmt.Sprintf("tcltest_%s_%s.golden", runtime.GOOS, runtime.GOARCH)))

	defer g.close()

	dir, err := ioutil.TempDir("", "tcl-test-")
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	if _, _, err := ccgo.CopyDir(dir, "testdata/tcl", nil); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ccgo.CopyDir(dir, "testdata/overlay", nil); err != nil {
		t.Log(err)
	}

	tcltest := filepath.Join(dir, "tcltest")
	if runtime.GOOS == "windows" {
		tcltest += ".exe"
	}
	args0 := []string{"build", "-o", tcltest}
	if s := *oXTags; s != "" {
		args0 = append(args0, "-tags", s)
	}
	args0 = append(args0, "modernc.org/tcl/internal/tcltest")
	cmd := exec.Command("go", args0...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s\n%v", out, err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"all.tcl",
		"-notfile", strings.Join(notFile, " "),
		"-skip", strings.Join(skip, " "),
	}
	if *oDebug != "" {
		args = append(args, "-debug", *oDebug)
	}
	if *oFile != "" {
		args = append(args, "-file", *oFile)
	}
	if *oMatch != "" {
		args = append(args, "-match", *oMatch)
	}
	if *oSingleProc {
		args = append(args, "-singleproc", "1")
	}
	if *oVerbose != "" {
		args = append(args, "-verbose", *oVerbose)
	}
	os.Setenv("TCL_LIBRARY", filepath.Join(pth, "assets"))
	os.Setenv("PATH", fmt.Sprintf("%s%c%s", dir, os.PathListSeparator, os.Getenv("PATH")))
	cmd = exec.Command(tcltest, args...)
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, g, os.Stdout)
	cmd.Stderr = io.MultiWriter(&out, g, os.Stdout)
	if err := cmd.Run(); err != nil {
		t.Error(err)
	}

	if b := out.Bytes(); bytes.Contains(b, []byte("FAIL")) ||
		bytes.Contains(b, []byte("Test file error:")) ||
		bytes.Contains(b, []byte("Test files exiting with errors:")) ||
		bytes.Contains(b, []byte("panic:")) {
		t.Error("failure(s) detected")
	}
}

type golden struct {
	t *testing.T
	f *os.File
	w *bufio.Writer
}

func newGolden(t *testing.T, fn string) *golden {
	f, err := os.Create(filepath.FromSlash(fn))
	if err != nil { // Possibly R/O fs in a VM
		base := filepath.Base(filepath.FromSlash(fn))
		f, err = ioutil.TempFile("", base)
		if err != nil {
			t.Fatal(err)
		}

	}
	t.Logf("writing results to %s\n", f.Name())
	w := bufio.NewWriter(f)
	return &golden{t, f, w}
}

func (g *golden) Write(b []byte) (int, error) {
	return g.w.Write(b)
}

func (g *golden) close() {
	if g.f == nil {
		return
	}

	if err := g.w.Flush(); err != nil {
		g.t.Fatal(err)
	}

	if err := g.f.Close(); err != nil {
		g.t.Fatal(err)
	}
}

func TestEval(t *testing.T) {
	in, err := NewInterp()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := in.Close(); err != nil {
			t.Error(err)
		}
	}()

	s, err := in.Eval("set a 42; incr a")
	if err != nil {
		t.Fatal(err)
	}

	if g, e := s, "43"; g != e {
		t.Errorf("got %q exp %q", g, e)
	}
}

func ExampleInterp_Eval() {
	in := MustNewInterp()
	s := in.MustEval(`

# This is the Tcl script
# ----------------------
set a 42
incr a
# ----------------------

`)
	in.MustClose()
	fmt.Println(s)
	// Output:
	// 43
}

func TestCreateCommand(t *testing.T) {
	in, err := NewInterp()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if in == nil {
			return
		}

		if err := in.Close(); err != nil {
			t.Error(err)
		}
	}()

	var delTrace string
	_, err = in.NewCommand(
		"::go::echo",
		func(clientData interface{}, in *Interp, args []string) int {
			args = append(args[1:], fmt.Sprint(clientData))
			in.SetResult(strings.Join(args, " "))
			return tcl.TCL_OK
		},
		42,
		func(clientData interface{}) {
			delTrace = fmt.Sprint(clientData)
		},
	)
	if err != nil {
		t.Error(err)
		return
	}

	s, err := in.Eval("::go::echo 123 foo bar")
	if err != nil {
		t.Fatal(err)
	}

	if g, e := s, "123 foo bar 42"; g != e {
		t.Errorf("got %q exp %q", g, e)
		return
	}

	err = in.Close()
	in = nil
	if err != nil {
		t.Error(err)
		return
	}

	if g, e := delTrace, "42"; g != e {
		t.Errorf("got %q exp %q", g, e)
	}
}

func ExampleInterp_NewCommand() {
	in := MustNewInterp()
	var delTrace string
	in.MustNewCommand(
		"::go::echo",
		func(clientData interface{}, in *Interp, args []string) int {
			// Go implementation of the Tcl ::go::echo command
			args = append(args[1:], fmt.Sprint(clientData))
			in.SetResult(strings.Join(args, " "))
			return tcl.TCL_OK
		},
		42, // client data
		func(clientData interface{}) {
			// Go implemetation of the command delete handler
			delTrace = fmt.Sprint(clientData)
		},
	)
	fmt.Println(in.MustEval("::go::echo 123 foo bar"))
	in.MustClose()
	fmt.Println(delTrace)
	// Output:
	// 123 foo bar 42
	// 42
}

func TestS390xBug(t *testing.T) {
	in, err := NewInterp()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := in.Close(); err != nil {
			t.Error(err)
		}
	}()

	s, err := in.Eval("set dirs {a b c d}; lindex $dirs end")
	if err != nil {
		t.Fatal(err)
	}

	if g, e := s, "d"; g != e {
		t.Errorf("got %q exp %q", g, e)
	}
}
