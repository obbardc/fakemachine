// +build linux
// +build amd64

package fakemachine

import (
	"fmt"
	"os"
	"syscall"
)

type umlBackend struct {
	baseBackend
	kernelPath      string
	moduleDir       string
	slirpHelperPath string
}

func newUmlBackend(m Machine) backend {
	b := &umlBackend{}
	b.name = "uml"
	b.machine = m

	// TODO are these paths ok on non-merged usr machine?

	// TODO /usr/bin/linux is setup by update-alternatives
	b.kernelPath = "/usr/bin/linux.uml"
	b.moduleDir = "/usr/lib/uml/modules"
	// TODO finalise slirp helper path
	b.slirpHelperPath = "/usr/bin/slirp-helper"

	// TODO remove chris debug paths
	base := "/home/obbardc/projects/debos/uml/"
	b.kernelPath = base + "linux-uml/linux"
	b.moduleDir = base + "linux-uml/modules/lib/modules"
	b.slirpHelperPath = base + "libslirp-rs/target/debug/libslirp-helper"

	return b
}

func (b *umlBackend) Supported() (bool, error) {
	// check the binaries are present
	if _, err := os.Stat(b.kernelPath); err != nil {
		return false, fmt.Errorf("user-mode-linux not installed")
	}
	if _, err := os.Stat(b.slirpHelperPath); err != nil {
		// TODO confirm package name
		return false, fmt.Errorf("slirp-helper not installed")
	}
	return true, nil
}

func (b *umlBackend) MachineImagePath(img image) string {
	return fmt.Sprintf("/dev/disk/by-path/platform-uml-blkdev.%d", img.index)
}

func (b *umlBackend) RequiredModules() []string {
	// TODO make sure no UML modules are needed in initrd
	return []string{}
}

func (b *umlBackend) KernelModulesDir() (string, error) {
	if _, err := os.Stat(b.moduleDir); err != nil {
		return "", err
	}

	return b.moduleDir, nil
}

func (b *umlBackend) StaticVolumes() []mountPoint {
	// mount the UML modules over the top of /lib/modules
	// which contains the modules from the base system
	// TODO seems OK on merged-usr system
	moduleVolume := mountPoint{hostDirectory: b.moduleDir, machineDirectory: "/lib/modules", label: "modules", static: true}
	return append(b.machine.staticVolumes(), moduleVolume)
}

func (b *umlBackend) NetworkdMatch() string {
	return "vec*"
}

func (b *umlBackend) MountParameters(m mountPoint) (fstype string, options []string) {
	fstype = "hostfs"
	options = []string{m.hostDirectory}
	return
}

func (b *umlBackend) JobOutputTTY() string {
	// Send the fakemachine job output to the right console
	if b.machine.showBoot {
		return "/dev/tty0"
	}
	return "/dev/tty1"
}

func (b *umlBackend) Start() (int, error) {
	// TODO commandWrapper forces network-wait-online & doesnt exit after network setup fail ?
	// TODO disable lvm2-activation-generator ?
	// TODO linux process is leftover when manually killing fakemachine. this bug also happens on KVM so should be tackled in follow-up
	// TODO killing helper doesn't stop fakemachine

	// TODO cleanup from here down
	// TODO remove useless qemuargs
	// TODO rename qemuargs to umlargs

	m := b.machine


	// create a socketpair
	// TODO syscall has been depreciated in favour of https://godoc.org/golang.org/x/sys
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return -1, err
	}

	// f1 is attached to the slirp-helper
	f1 := os.NewFile(uintptr(fds[0]), "")
	if f1 == nil {
		return -1, fmt.Errorf("socketpair: f1 is incorrect")
	}
	defer f1.Close()

	// f2 is attached to the uml guest
	f2 := os.NewFile(uintptr(fds[1]), "")
	if f2 == nil {
		return -1, fmt.Errorf("socketpair: f2 is incorrect")
	}
	defer f2.Close()


	// launch the slirp helper and attach to socketpair using --fd=0 argument
	slirp_args := []string{"libslirp-helper",
				"--fd=3",
				"--exit-with-parent"}
	slirp_pa := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr, f1},
	}

	p_slirp, err := os.StartProcess(b.slirpHelperPath, slirp_args, &slirp_pa)
	if err != nil {
		return -1, err
	}
	defer p_slirp.Kill()


	// launch uml guest
	memory := fmt.Sprintf("%d", m.memory)
	//numcpus := fmt.Sprintf("%d", m.numcpus)
	qemuargs := []string{"linux",
		//"-cpu", "host",
		//"-smp", numcpus,
		"mem=" + memory + "M",
		//"-enable-kvm",
		//"-kernel", "/boot/vmlinuz-" + kernelRelease,
		"initrd=" + m.initrdpath,
		"panic=-1",
		"nosplash",
		"systemd.unit=fakemachine.service",
		"console=tty0",
		//"vec0:transport=libslirp,dst=/tmp/libslirp",
		//"vec0:transport=bess,dst=/tmp/libslirp",
		"vec0:transport=fd,fd=3,vec=0",
		//"root=/dev/ram0",
		//"rootfstype=ramfs",
		//"rw",
		//"init=/init",
	}
	//kernelargs := []string{"console=ttyS0", "panic=-1",
	//"systemd.unit=fakemachine.service"}

	if m.showBoot {
		// Create a character device representing our stdio
		// file descriptors, and connect the emulated serial
		// port (which is the console device for the BIOS,
		// Linux and systemd, and is also connected to the
		// fakemachine script) to that device
		//qemuargs = append(qemuargs,
		//	"-chardev", "stdio,id=for-ttyS0,signal=off",
		//	"-serial", "chardev:for-ttyS0")
		qemuargs = append(qemuargs,
			"con0=fd:0,fd:1", // tty0 to stdin/stdout when showing boot
			"con=none")       // no other consoles
	} else {
		// don't show the UML message output by default
		qemuargs = append(qemuargs,
			"quiet")


		//qemuargs = append(qemuargs,
		// Create the bus for virtio consoles
		//	"-device", "virtio-serial",
		// Create /dev/ttyS0 to be the VM console, but
		// ignore anything written to it, so that it
		// doesn't corrupt our terminal
		//	"-chardev", "null,id=for-ttyS0",
		//	"-serial", "chardev:for-ttyS0",
		//	// Connect the fakemachine script to our stdio
		// file descriptors
		//	"-chardev", "stdio,id=for-hvc0,signal=off",
		//	"-device", "virtconsole,chardev=for-hvc0")
		qemuargs = append(qemuargs,
			"con1=fd:0,fd:1",
			"con0=null",
			"con=none")       // no other consoles
	}

	//for _, point := range m.mounts {
	//qemuargs = append(qemuargs, "-virtfs",
	//	fmt.Sprintf("local,mount_tag=%s,path=%s,security_model=none",
	//		point.label, point.hostDirectory))
	//}

	for i, img := range m.images {
		qemuargs = append(qemuargs,
			fmt.Sprintf("ubd%d=%s", i, img.path))
		//qemuargs = append(qemuargs, "-drive",
		//	fmt.Sprintf("file=%s,if=none,format=raw,cache=unsafe,id=drive-virtio-disk%d", img.path, i))
		//qemuargs = append(qemuargs, "-device",
		//	fmt.Sprintf("virtio-blk-pci,drive=drive-virtio-disk%d,id=virtio-disk%d,serial=%s",
		//		i, i, img.label))
	}

	pa := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr, f2},
	}

	p, err := os.StartProcess(b.kernelPath, qemuargs, &pa)
	if err != nil {
		return -1, err
	}

	// wait for uml process to exit
	ustate, err := p.Wait()
	if err != nil {
		return -1, err
	}

	return ustate.ExitCode(), nil
}
