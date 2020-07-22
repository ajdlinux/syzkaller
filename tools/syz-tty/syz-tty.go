// Copyright 2017 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

// syz-tty is utility for testing of usb console reading code. Usage:
//   $ syz-tty /dev/ttyUSBx
// This should dump device console output.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/google/syzkaller/prog"
	"github.com/google/syzkaller/vm/vmimpl"
)

var flagVersion = flag.Bool("version", false, "print program version information")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s [options] /dev/ttyUSBx\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *flagVersion {
		prog.PrintVersion()
		os.Exit(0)
	}
	if len(os.Args) != 2 {
		flag.Usage()
		os.Exit(1)
	}
	con, err := vmimpl.OpenConsole(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open console: %v\n", err)
		os.Exit(1)
	}
	defer con.Close()
	io.Copy(os.Stdout, con)
}
