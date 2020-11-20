// +build linux
// +build amd64

package fakemachine

import(
	"fmt"
	"os"
	"path"
)

func BackendNames() []string {
	return []string{"auto", "kvm"}
}

// A static backend has no machine or state associated with it, it is purely for
// determining whether the backend is supported on this machine.
func getStaticBackend(name string) (backend, error) {
	return newBackend(name, Machine{})
}

func newBackendFromMachine(m Machine) (backend, error) {
	return newBackend(m.backendName, m)
}

func newBackend(backendName string, m Machine) (backend, error) {
	var b backend

	switch backendName {
	case "auto":
		fallthrough
	case "kvm":
		b = newKvmBackend(m)
	default:
		return nil, fmt.Errorf("backend %s does not exist", backendName)
	}

	// check chosen backend is supported
	if supported, err := b.Supported(); !supported {
		return nil, fmt.Errorf("%s not supported: %v", backendName, err)
	}

	return b, nil
}

type backend interface {
	Name() string

	// returns whether this backed is supported on this machine; if the backend is unsupported
	// then an error is returned with the reason why it is unsupported.
	Supported() (bool, error)

	// returns the speculative internal path to an image. this is a special
	// function since the image path is required in the frontend before the fakemachine
	// is started.
	MachineImagePath(img image) string

	// returns a list of modules which are required to be installed in the initrd.
	RequiredModules() []string

	// returns the location of the kernel modules.
	KernelModulesDir() (string, error)

	// returns a list of modules which are to be probed in the initscript.
	InitModules() []string

	// returns a list of static volumes, to be mounted in the initscript.
	StaticVolumes() []mountPoint

	// returns the filesystem type and options for a specified mountpoint.
	MountParameters(m mountPoint) (fsType string, options []string)

	// returns the networkd match string for the network interfaces this backend provides.
	NetworkdMatch() string

	// returns the TTY where the job output should be posted to.
	JobOutputTTY() string

	// start the backend
	Start() (int, error)
}

type baseBackend struct {
	name                string
	machine             Machine
}

// return the name of the backend
func (b *baseBackend) Name() string {
	return b.name
}

func (b *baseBackend) RequiredModules() []string {
	return []string{}
}

// by default will return the kernel modules dir of the host kernel
func (b *baseBackend) KernelModulesDir() (string, error) {
	moddir := "/lib/modules"
	if mergedUsrSystem() {
		moddir = "/usr/lib/modules"
	}

	kernelRelease, err := hostKernelRelease()
	if err != nil {
		return "", err
	}

	moddir = path.Join(moddir, kernelRelease)
	if _, err := os.Stat(moddir); err != nil {
		return "", err
	}

	return moddir, nil
}

func (b *baseBackend) InitModules() []string {
	return []string{}
}

func (b *baseBackend) StaticVolumes() []mountPoint {
	return b.machine.staticVolumes()
}

func (b *baseBackend) NetworkdMatch() string {
	return "e*"
}
