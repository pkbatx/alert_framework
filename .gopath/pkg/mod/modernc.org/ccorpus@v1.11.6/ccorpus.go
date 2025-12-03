// Copyright 2021 The Ccorpus Authors. All rights reserved.
// Use of the source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate assets -package ccorpus
//go:generate gofmt -l -s -w assets.go

// Package ccorpus provides a test corpus of C code.
package ccorpus // import "modernc.org/ccorpus"

import (
	"time"

	"modernc.org/httpfs"
)

var fs = httpfs.NewFileSystem(assets, time.Now())

// FileSystem returns a httpfs.FileSystem containing the test corpus. Use the
// Open method of the result to acquire a http.File from a rooted file name.
func FileSystem() *httpfs.FileSystem { return fs }
