// Copyright 2021 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package openstack

import (
        "fmt"
        "io"
        "net"
        "os"
        "os/exec"
        "path/filepath"
        "strconv"
        "strings"
        "time"

	"github.com/gophercloud/gophercloud"

        "github.com/google/syzkaller/pkg/config"
        "github.com/google/syzkaller/pkg/log"
        "github.com/google/syzkaller/pkg/osutil"
        "github.com/google/syzkaller/pkg/report"
        "github.com/google/syzkaller/vm/vmimpl"
)

func init() {
        vmimpl.Register("openstack", ctor, false) // TODO: overcommit?
}
