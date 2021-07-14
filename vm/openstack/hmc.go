// HMC console access

// This module provides a workaround for the lack of console logging support in PowerVC.

// For HMC managed systems, we can log into the HMC directly via ssh, and get a console from there.

package openstack

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
	"strings"
	
	"github.com/google/syzkaller/pkg/log"
	"github.com/google/syzkaller/pkg/osutil"
	"github.com/google/syzkaller/vm/vmimpl"
	"github.com/google/syzkaller/sys/targets"
)

func (inst *instance) sshArgs(args ...string) []string {
	sshArgs := append(vmimpl.SSHArgs(inst.debug, inst.sshKey, 22), inst.sshUser+"@"+inst.ip)
	if inst.env.OS == targets.Linux && inst.sshUser != "root" {
		args = []string{"sudo", "bash", "-c", "'" + strings.Join(args, " ") + "'"}
	}
	return append(sshArgs, args...)
}

func (inst *instance) HMCRun(timeout time.Duration, stop <-chan bool, command string) (
	<-chan []byte, <-chan error, error) {
	// need to connect to HMC, run "mkvterm -m <hostname> -p <partition name>"
	// might need to run rmvterm first

	// ssh -t hscroot@<hmc address>

	// need: hmc address, hmc username, hmc password, partition name, vm host name
	// return: output byte channel, error channel, error

	conRpipe, conWpipe, err := osutil.LongPipe()
	if err != nil {
		return nil, nil, err
	}

	mkvtermArgs := fmt.Sprintf("mkvterm -m %s -p %s", inst.hypervisorHostname, inst.instanceName)
	conAddr := fmt.Sprintf("%s@%s", inst.hmcUsername, inst.hmcAddr)
	conArgs := []string{"sshpass", "-p", inst.hmcPassword, "ssh"}
	conArgs = append(conArgs, vmimpl.SSHArgs(inst.debug, "", 22)...)
	conArgs = append(conArgs, "-t", conAddr, mkvtermArgs)
	con := osutil.Command("sshpass", conArgs...)
	con.Env = []string{}
	con.Stdout = conWpipe
	con.Stderr = conWpipe
	conw, err := con.StdinPipe()
	if err != nil {
		conRpipe.Close()
		conWpipe.Close()
		return nil, nil, err
	}
	if inst.consolew != nil {
		inst.consolew.Close()
	}
	inst.consolew = conw
	if err := con.Start(); err != nil {
		conRpipe.Close()
		conWpipe.Close()
		return nil, nil, fmt.Errorf("failed to connect to console server: %v", err)
	}
	conWpipe.Close()

	// TODO gotta shove the password in somewhere

	var tee io.Writer
	if inst.debug {
		tee = os.Stdout
	}
	merger := vmimpl.NewOutputMerger(tee)
	if err := waitForConsoleConnect(merger); err != nil {
		con.Process.Kill()
		merger.Wait()
		return nil, nil, err
	}
	sshRpipe, sshWpipe, err := osutil.LongPipe()
	if err != nil {
		con.Process.Kill()
		merger.Wait()
		sshRpipe.Close()
		return nil, nil, err
	}
	ssh := osutil.Command("ssh", inst.sshArgs(command)...)
	ssh.Stdout = sshWpipe
	ssh.Stderr = sshWpipe
	if err := ssh.Start(); err != nil {
		con.Process.Kill()
		merger.Wait()
		sshRpipe.Close()
		sshWpipe.Close()
		return nil, nil, fmt.Errorf("failed to connect to instance: %v", err)
	}
	sshWpipe.Close()
	merger.Add("ssh", sshRpipe)

	errc := make(chan error, 1)
	signal := func(err error) {
		select {
		case errc <- err:
		default:
		}
	}

	go func() {
		select {
		case <-time.After(timeout):
			signal(vmimpl.ErrTimeout)
		case <-stop:
			signal(vmimpl.ErrTimeout)
		case <-inst.closed:
			signal(fmt.Errorf("instance closed"))
		case err := <-merger.Err:
			con.Process.Kill()
			ssh.Process.Kill()
			merger.Wait()
			con.Wait()
			if cmdErr := ssh.Wait(); cmdErr == nil {
				// If the command exited successfully, we got EOF error from merger.
				// But in this case no error has happened and the EOF is expected.
				err = nil
			} else if merr, ok := err.(vmimpl.MergerError); ok && merr.R == conRpipe {
				// Console connection must never fail. If it does, it's either
				// instance preemption or a GCE bug. In either case, not a kernel bug.
				log.Logf(1, "%v: gce console connection failed with %v", inst.name, merr.Err)
				err = vmimpl.ErrTimeout
			} else {
				//////// XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
				// Check if the instance was terminated due to preemption or host maintenance.
				time.Sleep(5 * time.Second) // just to avoid any GCE races
				//if !inst.GCE.IsInstanceRunning(inst.name) {
				//	log.Logf(1, "%v: ssh exited but instance is not running", inst.name)
				//	err = vmimpl.ErrTimeout
				//}
			}
			signal(err)
			return
		}
		con.Process.Kill()
		ssh.Process.Kill()
		merger.Wait()
		con.Wait()
		ssh.Wait()
	}()
	return merger.Output, errc, nil
}

func waitForConsoleConnect(merger *vmimpl.OutputMerger) error {
	// We've started the console reading ssh command, but it has not necessary connected yet.
	// If we proceed to running the target command right away, we can miss part
	// of console output. During repro we can crash machines very quickly and
	// would miss beginning of a crash. Before ssh starts piping console output,
	// it usually prints "Open Completed."
	// So we wait for this line, or at least a minute and at least some output.
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	connectedMsg := []byte("Open Completed.")
	permissionDeniedMsg := []byte("Permission denied")
	var output []byte
	for {
		select {
		case out := <-merger.Output:
			output = append(output, out...)
			if bytes.Contains(output, connectedMsg) {
				// Just to make sure (otherwise we still see trimmed reports).
				time.Sleep(5 * time.Second)
				return nil
			}
			if bytes.Contains(output, permissionDeniedMsg) {
				return fmt.Errorf("broken console: %s", permissionDeniedMsg)
			}
		case <-timeout.C:
			if len(output) == 0 {
				return fmt.Errorf("broken console: no output")
			}
			return nil
		}
	}
}
