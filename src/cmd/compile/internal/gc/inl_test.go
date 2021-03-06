// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"bufio"
	"internal/testenv"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestIntendedInlining tests that specific runtime functions are inlined.
// This allows refactoring for code clarity and re-use without fear that
// changes to the compiler will cause silent performance regressions.
func TestIntendedInlining(t *testing.T) {
	if testing.Short() && testenv.Builder() == "" {
		t.Skip("skipping in short mode")
	}
	testenv.MustHaveGoRun(t)
	t.Parallel()

	// want is the list of function names (by package) that should
	// be inlined.
	want := map[string][]string{
		"runtime": {
			// TODO(mvdan): enable these once mid-stack
			// inlining is available
			// "adjustctxt",

			"add",
			"addb",
			"adjustpanics",
			"adjustpointer",
			"bucketMask",
			"bucketShift",
			"chanbuf",
			"deferArgs",
			"deferclass",
			"evacuated",
			"fastlog2",
			"fastrand",
			"float64bits",
			"getm",
			"isDirectIface",
			"itabHashFunc",
			"maxSliceCap",
			"noescape",
			"readUnaligned32",
			"readUnaligned64",
			"round",
			"roundupsize",
			"stringStructOf",
			"subtractb",
			"tophash",
			"totaldefersize",
			"(*bmap).keys",
			"(*bmap).overflow",
			"(*waitq).enqueue",
		},
		"runtime/internal/sys": {},
		"bytes": {
			"(*Buffer).Bytes",
			"(*Buffer).Cap",
			"(*Buffer).Len",
			"(*Buffer).Next",
			"(*Buffer).Read",
			"(*Buffer).ReadByte",
			"(*Buffer).Reset",
			"(*Buffer).String",
			"(*Buffer).UnreadByte",
			"(*Buffer).tryGrowByReslice",
		},
		"unicode/utf8": {
			"FullRune",
			"FullRuneInString",
			"RuneLen",
			"ValidRune",
		},
	}

	if runtime.GOARCH != "386" {
		// nextFreeFast calls sys.Ctz64, which on 386 is implemented in asm and is not inlinable.
		// We currently don't have midstack inlining so nextFreeFast is also not inlinable on 386.
		// So check for it only on non-386 platforms.
		want["runtime"] = append(want["runtime"], "nextFreeFast")
		// As explained above, Ctz64 and Ctz32 are not Go code on 386.
		// The same applies to Bswap32.
		want["runtime/internal/sys"] = append(want["runtime/internal/sys"], "Ctz64")
		want["runtime/internal/sys"] = append(want["runtime/internal/sys"], "Ctz32")
		want["runtime/internal/sys"] = append(want["runtime/internal/sys"], "Bswap32")
	}
	switch runtime.GOARCH {
	case "amd64", "amd64p32", "arm64", "mips64", "mips64le", "ppc64", "ppc64le", "s390x":
		// rotl_31 is only defined on 64-bit architectures
		want["runtime"] = append(want["runtime"], "rotl_31")
	}

	notInlinedReason := make(map[string]string)
	pkgs := make([]string, 0, len(want))
	for pname, fnames := range want {
		pkgs = append(pkgs, pname)
		for _, fname := range fnames {
			fullName := pname + "." + fname
			if _, ok := notInlinedReason[fullName]; ok {
				t.Errorf("duplicate func: %s", fullName)
			}
			notInlinedReason[fullName] = "unknown reason"
		}
	}

	args := append([]string{"build", "-a", "-gcflags=-m -m"}, pkgs...)
	cmd := testenv.CleanCmdEnv(exec.Command(testenv.GoToolPath(t), args...))
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmdErr := make(chan error, 1)
	go func() {
		cmdErr <- cmd.Run()
		pw.Close()
	}()
	scanner := bufio.NewScanner(pr)
	curPkg := ""
	canInline := regexp.MustCompile(`: can inline ([^ ]*)`)
	cannotInline := regexp.MustCompile(`: cannot inline ([^ ]*): (.*)`)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			curPkg = line[2:]
			continue
		}
		if m := canInline.FindStringSubmatch(line); m != nil {
			fname := m[1]
			delete(notInlinedReason, curPkg+"."+fname)
			continue
		}
		if m := cannotInline.FindStringSubmatch(line); m != nil {
			fname, reason := m[1], m[2]
			fullName := curPkg + "." + fname
			if _, ok := notInlinedReason[fullName]; ok {
				// cmd/compile gave us a reason why
				notInlinedReason[fullName] = reason
			}
			continue
		}
	}
	if err := <-cmdErr; err != nil {
		t.Fatal(err)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for fullName, reason := range notInlinedReason {
		t.Errorf("%s was not inlined: %s", fullName, reason)
	}
}
