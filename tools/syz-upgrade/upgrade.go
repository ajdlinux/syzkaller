// Copyright 2015 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

// upgrade upgrades corpus from an old format to a new format.
// Upgrade is not fully automatic. You need to update prog.Serialize.
// Run the tool. Then update prog.Deserialize. And run the tool again that
// the corpus is not changed this time.
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/syzkaller/pkg/osutil"
	"github.com/google/syzkaller/prog"
	_ "github.com/google/syzkaller/sys"
)

var flagVersion = flag.Bool("version", false, "print program version information")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s [options] corpus_dir\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *flagVersion {
		prog.PrintVersion()
		os.Exit(0)
	}
	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(1)
	}
	files, err := ioutil.ReadDir(args[1])
	if err != nil {
		fatalf("failed to read corpus dir: %v", err)
	}
	target, err := prog.GetTarget(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fatalf("%v", err)
	}
	for _, f := range files {
		fname := filepath.Join(args[1], f.Name())
		data, err := ioutil.ReadFile(fname)
		if err != nil {
			fatalf("failed to read program: %v", err)
		}
		p, err := target.Deserialize(data, prog.NonStrict)
		if err != nil {
			fatalf("failed to deserialize program: %v", err)
		}
		data1 := p.Serialize()
		if bytes.Equal(data, data1) {
			continue
		}
		fmt.Printf("upgrading:\n%s\nto:\n%s\n\n", data, data1)
		hash := sha1.Sum(data1)
		fname1 := filepath.Join(args[1], hex.EncodeToString(hash[:]))
		if err := osutil.WriteFile(fname1, data1); err != nil {
			fatalf("failed to write program: %v", err)
		}
		if err := os.Remove(fname); err != nil {
			fatalf("failed to remove program: %v", err)
		}
	}
}

func fatalf(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}
