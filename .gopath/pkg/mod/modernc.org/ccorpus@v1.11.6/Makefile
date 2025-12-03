# Copyright 2021 The Ccorpus Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

.PHONY:	all bench clean cover cpu editor internalError later mem nuke todo edit devbench

grep=--include=*.go
ngrep='TODOOK\|internalError\|testdata\|scanner.go\|parser.go\|ast.go\|assets.go\|TODO-'


all:
	@LC_ALL=C date
	@go version 2>&1 | tee log
	@gofmt -l -s -w *.go
	@go install -v
	@go test 2>&1 -timeout 1h | tee -a log
	@go vet 2>&1 | grep -v $(ngrep) || true
	@golint 2>&1 | grep -v $(ngrep) || true
	@make todo
	@staticcheck | grep -v 'scanner\.go' || true
	@grep -n --color=always 'FAIL\|PASS' log 
	LC_ALL=C date 2>&1 | tee -a log

generate:
	go generate 2>&1 | tee log-generate
	gofmt -l -s -w *.go 2>&1 | tee -a log-generate
	go build -v 2>&1 | tee -a log-generate

devbench:
	date 2>&1 | tee log-devbench
	go test -timeout 24h -dev -run @ -bench . 2>&1 | tee -a log-devbench
	grep -n 'FAIL\|SKIP' log-devbench || true

bench:
	date 2>&1 | tee log-bench
	go test -timeout 24h -v -run '^[^E]' -bench . 2>&1 | tee -a log-bench
	grep -n 'FAIL\|SKIP' log-bench || true

clean:
	go clean
	rm -f *~ *.test *.out

cover:
	t=$(shell mktemp) ; go test -coverprofile $$t && go tool cover -html $$t && unlink $$t

cpu: clean
	go test -run @ -bench . -cpuprofile cpu.out
	go tool pprof -lines *.test cpu.out

edit:
	@touch log
	@if [ -f "Session.vim" ]; then gvim -S & else gvim -p Makefile *.go & fi

editor: generate
	gofmt -l -s -w *.go
	nilness .
	go install -v 2>&1 | tee log-install

later:
	@grep -n $(grep) LATER * || true
	@grep -n $(grep) MAYBE * || true

mem: clean
	go test -run @ -dev -bench . -memprofile mem.out -timeout 24h
	go tool pprof -lines -web -alloc_space *.test mem.out

nuke: clean
	go clean -i

todo:
	@grep -nr $(grep) ^[[:space:]]*_[[:space:]]*=[[:space:]][[:alpha:]][[:alnum:]]* * | grep -v $(ngrep) || true
	@grep -nrw $(grep) 'TODO\|panic' * | grep -v $(ngrep) || true
	@grep -nr $(grep) BUG * | grep -v $(ngrep) || true
	@grep -nr $(grep) [^[:alpha:]]println * | grep -v $(ngrep) || true
	@grep -nir $(grep) 'work.*progress' || true
