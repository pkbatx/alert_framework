# Copyright 2020 The Tcl Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

.PHONY:	all clean cover cpu editor internalError later mem nuke todo edit tcl gotclsh

grep=--include=*.go --include=*.l --include=*.y --include=*.yy
ngrep='TODOOK\|internal\|.*stringer.*\.go\|assets\.go'
host=$(shell go env GOOS)-$(shell go env GOARCH)
testlog=testdata/testlog-$(shell echo $$GOOS)-$(shell echo $$GOARCH)-on-$(shell go env GOOS)-$(shell go env GOARCH)

all: editor
	date
	go version 2>&1 | tee log
	./unconvert.sh
	gofmt -l -s -w *.go
	go test -i
	go test 2>&1 -timeout 1h | tee -a log
	GOOS=darwin GOARCH=amd64 go build -o /dev/null
	GOOS=darwin GOARCH=arm64 go build -o /dev/null
	GOOS=freebsd GOARCH=arm64 go build -o /dev/null
	GOOS=linux GOARCH=386 go build -o /dev/null
	GOOS=linux GOARCH=amd64 go build -o /dev/null
	GOOS=linux GOARCH=arm go build -o /dev/null
	GOOS=linux GOARCH=arm64 go build -o /dev/null
	GOOS=linux GOARCH=riscv64 go build -o /dev/null
	GOOS=linux GOARCH=s390x go build -o /dev/null
	GOOS=netbsd GOARCH=arm64 go build -o /dev/null
	GOOS=openbsd GOARCH=arm64 go build -o /dev/null
	GOOS=windows GOARCH=386 go build -o /dev/null
	GOOS=windows GOARCH=amd64 go build -o /dev/null
	GOOS=windows GOARCH=arm64 go build -o /dev/null
	go vet 2>&1 | grep -v $(ngrep) || true
	golint 2>&1 | grep -v $(ngrep) || true
	#make todo
	misspell *.go | grep -v $(ngrep) || true
	staticcheck
	maligned || true
	grep -n 'FAIL\|PASS' log
	git diff --unified=0 testdata/*.golden || true
	grep -n Passed log
	go version
	date 2>&1 | tee -a log

# generate on current host
generate:
	go generate 2>&1 | tee log-generate
	go build -v ./... 

gotclsh:
	go install -v modernc.org/tcl/gotclsh && \
		ls -l $$(which gotclsh) && \
		go version -m $$(which gotclsh)

build_all_targets:
	GOOS=darwin GOARCH=amd64 go build -v ./...
	GOOS=darwin GOARCH=amd64 go test -c -o /dev/null
	GOOS=darwin GOARCH=arm64 go build -v ./...
	GOOS=darwin GOARCH=arm64 go test -c -o /dev/null
	GOOS=freebsd GOARCH=386 go build -v ./...
	GOOS=freebsd GOARCH=386 go test -c -o /dev/null
	GOOS=freebsd GOARCH=amd64 go build -v ./...
	GOOS=freebsd GOARCH=amd64 go test -c -o /dev/null
	GOOS=freebsd GOARCH=arm go build -v ./...
	GOOS=freebsd GOARCH=arm go test -c -o /dev/null
	GOOS=freebsd GOARCH=arm64 go build -v ./...
	GOOS=freebsd GOARCH=arm64 go test -c -o /dev/null
	GOOS=linux GOARCH=386 go build -v ./...
	GOOS=linux GOARCH=386 go test -c -o /dev/null
	GOOS=linux GOARCH=amd64 go build -v ./...
	GOOS=linux GOARCH=amd64 go test -c -o /dev/null
	GOOS=linux GOARCH=arm go build -v ./...
	GOOS=linux GOARCH=arm go test -c -o /dev/null
	GOOS=linux GOARCH=arm64 go build -v ./...
	GOOS=linux GOARCH=arm64 go test -c -o /dev/null
	GOOS=linux GOARCH=ppc64le go build -v ./...
	GOOS=linux GOARCH=ppc64le go test -c -o /dev/null
	GOOS=linux GOARCH=riscv64 go build -v ./...
	GOOS=linux GOARCH=riscv64 go test -c -o /dev/null
	GOOS=linux GOARCH=s390x go build -v ./...
	GOOS=linux GOARCH=s390x go test -c -o /dev/null
	GOOS=openbsd GOARCH=amd64 go build -v ./...
	GOOS=openbsd GOARCH=amd64 go test -c -o /dev/null
	GOOS=openbsd GOARCH=arm64 go build -v ./...
	GOOS=openbsd GOARCH=arm64 go test -c -o /dev/null
	GOOS=windows GOARCH=386 go build -v ./...
	GOOS=windows GOARCH=386 go test -c -o /dev/null
	GOOS=windows GOARCH=amd64 go build -v ./...
	GOOS=windows GOARCH=amd64 go test -c -o /dev/null
	GOOS=windows GOARCH=arm64 go build -v ./...
	GOOS=windows GOARCH=arm64 go test -c -o /dev/null
	echo done

darwin_amd64:
	@echo "Should be executed only on darwin/amd64."
	go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

darwin_arm64:
	@echo "Should be executed only on darwin/arm64."
	AR=$$(which ar) go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

freebsd_amd64:
	@echo "Should be executed only on freebsd/amd64."
	AR=$$(which ar) go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

freebsd_386_config:
	@echo "Should be executed only on freebsd/386."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

freebsd_386_pull:
	@echo "Should be executed only on freebsd/amd64 after manually pulling and pushing tcl/tmp freebsd-386 -> 3900x -> freebsd64"
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=freebsd TARGET_GOARCH=386 go generate 2>&1 | tee log-generate
	GOOS=freebsd GOARCH=386 go build -v ./... 2>&1 | tee -a log-generate

freebsd_arm_config:
	@echo "Should be executed only on freebsd/arm."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

freebsd_arm_pull:
	@echo "Should be executed only on freebsd/amd64 after manually pulling and pushing tcl/tmp freebsd-arm -> 3900x -> freebsd64"
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=freebsd TARGET_GOARCH=arm go generate 2>&1 | tee log-generate
	GOOS=freebsd GOARCH=arm go build -v ./... 2>&1 | tee -a log-generate

netbsd_amd64:
	@echo "Should be executed only on netbsd/amd64."
	AR=$$(which ar) go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

linux_amd64:
	@echo "Should be executed only on linux/amd64."
	go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

linux_386_config:
	@echo "Should be executed only on linux/386."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

linux_386_pull:
	@echo "Can be executed everywhere"
	rm -rf tmp/
	mkdir tmp/
	rsync -rp nuc32:src/modernc.org/tcl/tmp/ tmp/
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=linux TARGET_GOARCH=386 go generate 2>&1 | tee log-generate
	GOOS=linux GOARCH=386 go build -v ./... 2>&1 | tee -a log-generate

devuan4-32_pull:
	@echo "Can be executed everywhere"
	rm -rf tmp/
	mkdir tmp/
	rsync -rp devuan4-32:src/modernc.org/tcl/tmp/ tmp/
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=linux TARGET_GOARCH=386 go generate 2>&1 | tee log-generate
	GOOS=linux GOARCH=386 go build -v ./... 2>&1 | tee -a log-generate

linux_arm_config:
	@echo "Should be executed only on linux/arm."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

linux_arm_pull:
	@echo "Can be executed everywhere"
	rm -rf tmp/
	mkdir tmp/
	rsync -rp pi32:src/modernc.org/tcl/tmp/ tmp/
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=linux TARGET_GOARCH=arm go generate 2>&1 | tee log-generate
	GOOS=linux GOARCH=arm go build -v ./... 2>&1 | tee -a log-generate

linux_arm64_config:
	@echo "Should be executed only on linux/arm64."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

linux_arm64_pull:
	@echo "Can be executed everywhere"
	rm -rf tmp/
	mkdir tmp/
	rsync -rp pi64:src/modernc.org/tcl/tmp/ tmp/
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_LOAD_CONFIG=tmp/config TARGET_GOOS=linux TARGET_GOARCH=arm64 go generate 2>&1 | tee log-generate
	GOOS=linux GOARCH=arm64 go build -v ./... 2>&1 | tee -a log-generate

linux_ppc64le:
	go run addport.go linux_amd64 linux_ppc64le
	GOOS=linux GOARCH=ppc64le go build -v ./... 2>&1 | tee -a log-generate

linux_riscv64:
	@echo "Should be executed only on linux/riscv64."
	go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

linux_s390x_config:
	@echo "Should be executed only on linux/s390x."
	rm -rf tmp/
	mkdir -p tmp/config tmp/src
	GO_GENERATE_TMPDIR=tmp/src GO_GENERATE_SAVE_CONFIG=tmp/config go generate 2>&1 | tee log-generate

linux_s390x_pull:
	@echo "Can be executed everywhere with enough RAM."
	rm -rf /home/${S390XVM_USER}/*
	mkdir -p /home/${S390XVM_USER}/src/modernc.org/tcl/tmp/ || true
	rsync -rp ${S390XVM}:src/modernc.org/tcl/tmp/ /home/${S390XVM_USER}/src/modernc.org/tcl/tmp/
	\
		GO_GENERATE_TMPDIR=/home/${S390XVM_USER}/src/modernc.org/tcl/tmp/src \
		GO_GENERATE_LOAD_CONFIG=/home/${S390XVM_USER}/src/modernc.org/tcl/tmp/config \
		TARGET_GOOS=linux TARGET_GOARCH=s390x go generate 2>&1 | tee log-generate
	GOOS=linux GOARCH=s390x go build -v ./... 2>&1 | tee -a log-generate

openbsd_amd64:
	@echo "Should be executed only on openbsd/amd64."
	AR=$$(which ar) go generate 2>&1 | tee log-generate
	go build -v ./... 2>&1 | tee -a log-generate

openbsd_arm64:
	go run addport openbsd_amd64 openbsd_arm64

windows_amd64:
	@echo "Should be executed only on linux/amd64."
	GO_GENERATE_CC=x86_64-w64-mingw32-gcc CCGO_CPP=x86_64-w64-mingw32-cpp TARGET_GOOS=windows TARGET_GOARCH=amd64 go generate 2>&1 | tee log-generate
	GOOS=windows GOARCH=amd64 go build -v ./... 2>&1 | tee -a log-generate

windows_386:
	GO_GENERATE_CC=i686-w64-mingw32-gcc CCGO_CPP=i686-w64-mingw32-cpp TARGET_GOOS=windows TARGET_GOARCH=386 go generate 2>&1 | tee log-generate
	GOOS=windows GOARCH=386 go build -v ./... 2>&1 | tee -a log-generate

windows_arm64:
	go run addport.go windows_amd64 windows_arm64
	GOOS=windows GOARCH=arm64 go build -v ./... 2>&1 | tee -a log-generate

test:
	go version | tee $(testlog)
	uname -a | tee -a $(testlog)
	go test -v -timeout 24h | tee -a $(testlog)
	grep -ni fail $(testlog) | tee -a $(testlog) || true
	LC_ALL=C date | tee -a $(testlog)
	grep -ni --color=always fail $(testlog) || true

test_darwin_amd64:
	GOOS=darwin GOARCH=amd64 make test

test_darwin_arm64:
	GOOS=darwin GOARCH=arm64 make test

test_linux_amd64:
	GOOS=linux GOARCH=amd64 make test

test_linux_386:
	GOOS=linux GOARCH=386 make test

test_linux_386_hosted:
	GOOS=linux GOARCH=386 make test

test_linux_arm:
	GOOS=linux GOARCH=arm make test

test_linux_arm64:
	GOOS=linux GOARCH=arm64 make test

test_linux_s390x:
	GOOS=linux GOARCH=s390x make test

test_windows_amd64:
	rm -f y:\\libc.log
	go version | tee %TEMP%\testlog-windows-amd64
	go test -v -timeout 24h | tee -a %TEMP%\testlog-windows-amd64
	date /T | tee -a %TEMP%\testlog-windows-amd64
	time /T | tee -a %TEMP%\testlog-windows-amd64

tmp: #TODO-
	cls
	go test -v -timeout 24h -run Tcl -verbose "start pass error" | tee -a %TEMP%\testlog-windows-amd64

clean:
	go clean
	rm -f *~ *.test *.out test.db* tt4-test*.db* test_sv.* testdb-*

cover:
	t=$(shell tempfile) ; go test -coverprofile $$t && go tool cover -html $$t && unlink $$t

cpu: clean
	go test -run @ -bench . -cpuprofile cpu.out
	go tool pprof -lines *.test cpu.out

edit:
	@touch log
	@if [ -f "Session.vim" ]; then gvim -S & else gvim -p Makefile *.go & fi

editor:
	gofmt -l -s -w *.go
	GO111MODULE=off go install -v ./...
	GO111MODULE=off go build -o /dev/null generator.go

internalError:
	egrep -ho '"internal error.*"' *.go | sort | cat -n

later:
	@grep -n $(grep) LATER * || true
	@grep -n $(grep) MAYBE * || true

mem: clean
	go test -run Mem -mem -memprofile mem.out -timeout 24h
	go tool pprof -lines -web -alloc_space *.test mem.out

memgrind:
	GO111MODULE=off go test -v -timeout 24h -tags libc.memgrind -xtags=libc.memgrind

nuke: clean
	go clean -i

todo:
	@grep -nr $(grep) ^[[:space:]]*_[[:space:]]*=[[:space:]][[:alpha:]][[:alnum:]]* * | grep -v $(ngrep) || true
	@grep -nr $(grep) TODO * | grep -v $(ngrep) || true
	@grep -nr $(grep) BUG * | grep -v $(ngrep) || true
	@grep -nr $(grep) [^[:alpha:]]println * | grep -v $(ngrep) || true

tcl:
	cp log log-0
	go test -run Tcl$$ 2>&1 -timeout 24h -trc | tee log
	grep -c '\.\.\. \?Ok' log || true
	grep -c '^!' log || true
	# grep -c 'Error:' log || true

tclshort:
	cp log log-0
	go test -run Tcl$$ -short 2>&1 -timeout 24h -trc | tee log
	grep -c '\.\.\. \?Ok' log || true
	grep -c '^!' log || true
	# grep -c 'Error:' log || true
