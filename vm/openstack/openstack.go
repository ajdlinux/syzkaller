// Copyright 2021 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package openstack

import (
        "fmt"
        "io"
//	"io/ioutil"
//        "net"
//        "os"
//        "os/exec"
        "path/filepath"
//        "strconv"
//        "strings"
        "time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/extendedserverattributes"

        "github.com/google/syzkaller/pkg/config"
        "github.com/google/syzkaller/pkg/log"
        "github.com/google/syzkaller/pkg/osutil"
        "github.com/google/syzkaller/pkg/report"
        "github.com/google/syzkaller/vm/vmimpl"
)

/*

What does the configuration look like?

How do we talk to Nova?

How do we establish an SSH link?

How do we establish a console link?

How do we set up port forwarding??? (GCE assumes you're on the same internal network, Qemu seems to use SSH port forwarding)

How do we get debugging information?

*/

func init() {
        vmimpl.Register("openstack", ctor, false) // TODO: overcommit?
}

type Config struct {
	// VM setup
	Count int `json:"count"` // number of VMs
	Flavor string `json:"flavor"` // flavor/machine type

	// image paths
	Image string `json:"image"` // image name

	// Cloud details
	Cloud *clientconfig.Cloud `json:"cloud"`
}

type Pool struct {
	env *vmimpl.Env
	cfg *Config
	computeClient *gophercloud.ServiceClient
	imageClient *gophercloud.ServiceClient
}

type serverAttributesExt struct { // XXX: PowerVC HMC hack
  servers.Server
  extendedserverattributes.ServerAttributesExt
}

type instance struct {
	env *vmimpl.Env
	cfg *Config
	computeClient *gophercloud.ServiceClient
	debug bool
	name string
	server serverAttributesExt
	ip string
	sshKey string
	sshUser string
	closed chan bool
	consolew io.WriteCloser

	// HMC console hack
	hypervisorHostname string
	instanceName string
	hmcAddr string
	hmcUsername string
	hmcPassword string
}

func findFlavor(computeClient *gophercloud.ServiceClient, flavorName string) *flavors.Flavor {
	listOpts := flavors.ListOpts{}
	allPages, err := flavors.ListDetail(computeClient, listOpts).AllPages()
	if err != nil {
		panic(err)
	}

	allFlavors, err := flavors.ExtractFlavors(allPages)
	if err != nil {
		panic(err)
	}

	for _, flavor := range allFlavors {
		if flavor.Name == flavorName {
			return &flavor
		}
	}
	
	return nil
}

func findServer(computeClient *gophercloud.ServiceClient, serverName string) *servers.Server {
	listOpts := servers.ListOpts{}
	allPages, err := servers.List(computeClient, listOpts).AllPages()
	if err != nil {
		panic(err)
	}

	allServers, err := servers.ExtractServers(allPages)
	if err != nil {
		panic(err)
	}

	for _, server := range allServers {
		if server.Name == serverName {
			return &server
		}
	}
	
	return nil
}

func findImage(imageClient *gophercloud.ServiceClient, imageName string) *images.Image {
	listOpts := images.ListOpts{}
	allPages, err := images.List(imageClient, listOpts).AllPages()
	if err != nil {
		panic(err)
	}

	allImages, err := images.ExtractImages(allPages)
	if err != nil {
		panic(err)
	}

	for _, image := range allImages {
		if image.Name == imageName {
			return &image
		}
	}
	
	return nil
}

// XXX
func TestCtor(env *vmimpl.Env) (vmimpl.Pool, error) {
	return ctor(env)
}

func ctor(env *vmimpl.Env) (vmimpl.Pool, error) {
	if env.Name == "" {
		return nil, fmt.Errorf("config param name is empty (required for OpenStack)")
	}
	cfg := &Config{
		Count: 1,
		Flavor: "",
		Cloud: &clientconfig.Cloud {
		},
	}
	if err := config.LoadData(env.Config, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse openstack vm config: %v", err)
	}
	if cfg.Count < 1 || cfg.Count > 1000 {
		return nil, fmt.Errorf("invalid config param count: %v, want [1, 1000]", cfg.Count)
	}
	if env.Debug && cfg.Count > 1 {
		log.Logf(0, "limiting number of VMs from %v to 1 in debug mode", cfg.Count)
		cfg.Count = 1
	}
	if cfg.Flavor == "" {
		return nil, fmt.Errorf("flavor parameter is empty")
	}
	if cfg.Image == "" {
		return nil, fmt.Errorf("image parameter is empty")
	}
/*	if cfg.GCEImage == "" && cfg.GCSPath == "" {
		return nil, fmt.Errorf("gcs_path parameter is empty")
	}
	if cfg.GCEImage == "" && env.Image == "" {
		return nil, fmt.Errorf("config param image is empty (required for GCE)")
	}
	if cfg.GCEImage != "" && env.Image != "" {
		return nil, fmt.Errorf("both image and gce_image are specified")
	} */

	yamlOpts := FakeYAMLOpts{
		Cloud: *cfg.Cloud, // TODO: verify Clouds?
	}
	opts := &clientconfig.ClientOpts{
		YAMLOpts: yamlOpts,
		Cloud: "default",
	}
	fmt.Printf("%+v\n", opts) // XXX
	fmt.Printf("%+v\n", cfg.Cloud) // XXX

	computeClient, err := clientconfig.NewServiceClient("compute", opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %v", err)
	}

	imageClient, err := clientconfig.NewServiceClient("image", opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create image service client: %v", err)
	}

	// TODO: Uploading image/new kernel etc

	pool := &Pool{
		cfg: cfg,
		env: env,
		computeClient: computeClient,
		imageClient: imageClient,
	}
	return pool, nil
}

func (pool *Pool) Count() int {
	return pool.cfg.Count
}

func (pool *Pool) Create(workdir string, index int) (vmimpl.Instance, error) {
	var server serverAttributesExt
	image := findImage(pool.imageClient, pool.cfg.Image)
	if image == nil {
		return nil, fmt.Errorf("couldn't find image '%s'", pool.cfg.Image)
	}

	flavor := findFlavor(pool.computeClient, pool.cfg.Flavor)
	if flavor == nil {
		return nil, fmt.Errorf("couldn't find flavor '%s'", pool.cfg.Flavor)
	}

	name := fmt.Sprintf("%v-%v", pool.env.Name, index)
	sshKey := filepath.Join(workdir, "key") // XXX: need to handle the case where the image doesn't take SSH key magic, like in GCE
	keygen := osutil.Command("ssh-keygen", "-t", "rsa", "-b", "2048", "-N", "", "-C", "syzkaller", "-f", sshKey)
	if out, err := keygen.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to execute ssh-keygen: %v\n%s", err, out)
	}
// XXXXXX
	//	sshKeyPub, err := ioutil.ReadFile(sshKey + ".pub")
//	if err != nil {
//		return nil, fmt.Errorf("failed to read file: %v", err)
	//	}
	// TODO: clean up the ssh keys elsewhere... see how GCE does this
	
	createOpts := servers.CreateOpts{
		Name: name,
		ImageRef: image.ID,
		FlavorRef: flavor.ID,
	}

	err := servers.Create(pool.computeClient, createOpts).ExtractInto(&server)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %v", err)
	}
	fmt.Printf("%v\n", server)
	serverID := server.ID

	sshUser := "syzkaller" // TODO get from config

	/// TODO: wait for ssh...
	for server.Status != "ACTIVE" {

		fmt.Printf("server ID: %s\n", serverID)
		err := servers.Get(pool.computeClient, serverID).ExtractInto(&server)
		if err != nil {
			return nil, fmt.Errorf("failed to get server details: %v", err)
		}
		//fmt.Printf("serverWithExt: %+v\n", serverWithExt)
		
		fmt.Printf("server: %+v\n", server)
		fmt.Printf("Name: %s\n", server.Name)
		fmt.Printf("Status: %s\n", server.Status)
		fmt.Printf("IP address: %s\n", server.AccessIPv4)
		
		fmt.Printf("Host ID: %s\n", server.HostID)
		fmt.Printf("HypervisorHostname: %s\n", server.HypervisorHostname)
		fmt.Printf("InstanceName: %s\n", server.InstanceName)

		fmt.Printf("=================================\n")
		
		time.Sleep(2*time.Second)
	}

	// XXX
	pool.env.Debug = true
	inst := &instance{
		env: pool.env,
		cfg: pool.cfg,
		computeClient: pool.computeClient,
		debug: pool.env.Debug,
		server: server,
		name: name,
		ip: server.AccessIPv4,
		sshKey: sshKey,
		sshUser: sshUser,
		closed: make(chan bool),

		hmcPassword: "ltc0zlabs", // TODO get from config
		hmcAddr: "hmc6.ozlabs.ibm.com", // TODO can we get this from the API?
		hmcUsername: "hscroot",
		hypervisorHostname: server.HypervisorHostname,
		instanceName: server.InstanceName,
	}

	return inst, nil
}

func runCmd(debug bool, bin string, args ...string) error {
	if debug {
		log.Logf(0, "running command: %v %#v", bin, args)
	}
	output, err := osutil.RunCmd(time.Minute, "", bin, args...)
	if debug {
		log.Logf(0, "result: %v\n%s", err, output)
	}
	return err
}

func (inst *instance) Copy(hostSrc string) (string, error) {
	vmDst := "./" + filepath.Base(hostSrc)
	args := append(vmimpl.SCPArgs(inst.debug, inst.sshKey, 22), hostSrc, inst.sshUser+"@"+inst.ip+":"+vmDst)
	if err := runCmd(inst.debug, "scp", args...); err != nil {
		return "", err
	}
	return vmDst, nil
}

func (inst *instance) Forward(port int) (string, error) {
	// TODO
	// What does GCE.InternalIP mean in the GCE version?
	
	panic("unimplemented")
}

func (inst *instance) Run(timeout time.Duration, stop <-chan bool, command string) (outc <-chan []byte, errc <-chan error, err error) {
	// TODO
	return inst.HMCRun(timeout, stop, command)
	panic("unimplemented")
}

func (inst *instance) Diagnose(rep *report.Report) (diagnosis []byte, wait bool) {
	// TODO
	panic("unimplemented")
}

func (inst *instance) Close() {
	close(inst.closed)
	err := servers.Delete(inst.computeClient, inst.server.ID).ExtractErr()
	if err != nil {
		// report and continue on anyway
		fmt.Errorf("error deleting instance '%s': %v\n", inst.name, err)
	}
	if inst.consolew != nil {
		inst.consolew.Close()
	}
}

// func (inst *instance) Info() ([]byte, error) {
// }

// lol "yaml"
type FakeYAMLOpts struct {
	Cloud clientconfig.Cloud
}

func (opts FakeYAMLOpts) LoadCloudsYAML() (map[string]clientconfig.Cloud, error) {
	clouds := make(map[string]clientconfig.Cloud)
	clouds["default"] = opts.Cloud
	return clouds, nil
}

func (opts FakeYAMLOpts) LoadSecureCloudsYAML() (map[string]clientconfig.Cloud, error) {
	return nil, nil
}

func (opts FakeYAMLOpts) LoadPublicCloudsYAML() (map[string]clientconfig.Cloud, error) {
	return nil, nil
}
