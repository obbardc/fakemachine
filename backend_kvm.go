// +build linux
// +build amd64

package fakemachine

import (
	"fmt"
	"os"
	"strings"
)

type kvmBackend struct {
	baseBackend
}

func newKvmBackend(m Machine) backend {
	b := &kvmBackend{}
	b.name = "kvm"
	b.machine = m
	return b
}

func (b *kvmBackend) Supported() (bool, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false, err
	}
	return true, nil
}

func (b *kvmBackend) MachineImagePath(img image) string {
	return fmt.Sprintf("/dev/disk/by-id/virtio-%s", img.label)
}

func (b *kvmBackend) RequiredModules() []string {
	return []string{"kernel/drivers/char/virtio_console.ko",
			"kernel/drivers/virtio/virtio.ko",
			"kernel/drivers/virtio/virtio_pci.ko",
			"kernel/net/9p/9pnet.ko",
			"kernel/drivers/virtio/virtio_ring.ko",
			"kernel/fs/9p/9p.ko",
			"kernel/net/9p/9pnet_virtio.ko",
			"kernel/fs/fscache/fscache.ko"}
}

func (b *kvmBackend) InitModules() []string {
	return []string{"virtio_pci", "virtio_console", "9pnet_virtio", "9p"}
}

func (b *kvmBackend) MountParameters(mount mountPoint) (fstype string, options []string) {
	fstype = "9p"
	options = []string{"trans=virtio", "version=9p2000.L", "cache=loose", "msize=262144"}
	return
}

func (b *kvmBackend) JobOutputTTY() string {
	// By default we send job output to the second virtio console,
	// reserving /dev/ttyS0 for boot messages (which we ignore)
	// and /dev/hvc0 for possible use by systemd as a getty
	// (which we also ignore).
	// If we are debugging, mix job output into the normal
	// console messages instead, so we can see both.
	if b.machine.showBoot {
		return "/dev/console"
	}
	return "/dev/hvc0"
}

func (b *kvmBackend) Start() (int, error) {
	m := b.machine

	kernelPath, err := hostKernelPath()
	if err != nil {
		return -1, err
	}
	memory := fmt.Sprintf("%d", m.memory)
	numcpus := fmt.Sprintf("%d", m.numcpus)
	qemuargs := []string{"qemu-system-x86_64",
		"-cpu", "host",
		"-smp", numcpus,
		"-m", memory,
		"-enable-kvm",
		"-kernel", kernelPath,
		"-initrd", m.initrdpath,
		"-display", "none",
		"-no-reboot"}
	kernelargs := []string{"console=ttyS0", "panic=-1",
		"systemd.unit=fakemachine.service"}

	if m.showBoot {
		// Create a character device representing our stdio
		// file descriptors, and connect the emulated serial
		// port (which is the console device for the BIOS,
		// Linux and systemd, and is also connected to the
		// fakemachine script) to that device
		qemuargs = append(qemuargs,
			"-chardev", "stdio,id=for-ttyS0,signal=off",
			"-serial", "chardev:for-ttyS0")
	} else {
		qemuargs = append(qemuargs,
			// Create the bus for virtio consoles
			"-device", "virtio-serial",
			// Create /dev/ttyS0 to be the VM console, but
			// ignore anything written to it, so that it
			// doesn't corrupt our terminal
			"-chardev", "null,id=for-ttyS0",
			"-serial", "chardev:for-ttyS0",
			// Connect the fakemachine script to our stdio
			// file descriptors
			"-chardev", "stdio,id=for-hvc0,signal=off",
			"-device", "virtconsole,chardev=for-hvc0")
	}

	for _, point := range m.mounts {
		qemuargs = append(qemuargs, "-virtfs",
			fmt.Sprintf("local,mount_tag=%s,path=%s,security_model=none",
				point.label, point.hostDirectory))
	}

	for i, img := range m.images {
		qemuargs = append(qemuargs, "-drive",
			fmt.Sprintf("file=%s,if=none,format=raw,cache=unsafe,id=drive-virtio-disk%d", img.path, i))
		qemuargs = append(qemuargs, "-device",
			fmt.Sprintf("virtio-blk-pci,drive=drive-virtio-disk%d,id=virtio-disk%d,serial=%s",
				i, i, img.label))
	}

	qemuargs = append(qemuargs, "-append", strings.Join(kernelargs, " "))

	pa := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}

	p, err := os.StartProcess("/usr/bin/qemu-system-x86_64", qemuargs, &pa)
	if err != nil {
		return -1, err
	}

	// wait for kvm process to exit
	pstate, err := p.Wait()
	if err != nil {
		return -1, err
	}

	return pstate.ExitCode(), nil
}
