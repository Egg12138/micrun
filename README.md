# todos
## todos in days

* yocto all-in-one building
* fix io bugs
* better netns handling
* fix metrics bugs

## todos in weeks

to consider:

* is it worthy to implementa micrun a **common runtime** which is capable of dealing with Linux OCI images?
> 1. ~~implement it manually~~
> 2. send request to other runtimes: lcr, runc, crun, gvisor, youki, etc.. , filter by annotations
     if annotations contain neither `defs.MicranAnnotationPrefix` nor `Infra container annotation`, transfer tasks to external runtime
     "crun or lcr" would be good for embedded system
> 3. 

# micrun container runimte


## register runtime

* by `--runtime io.containerd.<runtime name>` options,  user can specify the runtime to run a container if runtime is installed on `$PATH`. 
* we can use containerd shim runtime [without installing on PATH](https://docs.docker.com/engine/daemon/alternative-runtimes/#use-a-containerd-shim-without-installing-on-path)

### registr  

### register on containerd

generally, add a new plugin `/etc/containerd/config.toml`:

```
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micran."]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  # remains empty
  # micrun configuration option design is unstable
  # we remove those codes in mcs repo 
```

### registr as a kubernetes runtimeclass

```yaml
version: v1
runtimeClass:
  name: micrun
  type: RuntimeClass

```

```shell
kubelet --cpu-manager-policy=static
# isolcpus, nohz_full, ... can be customized
```

is recommened


## use runtime

use nerdctl

```shell
# notice, in nerdctl '--label' option is NOT "Label" in docker, it is "Annotation"
# Hence -l option pass annotation to container oci config
nerdctl run -d --runtime io.containerd.mica.v2 -l org.openeuler.micran.auto_disconnect=true <image>  
nerdctl update --memory 1024m  <contaienr_id>
```

use ctr (containerd test tool for developer)

```shell
ctr container create --runtime io.containerd.mica.v2 -t --annotation org.openeuler.micran.auto_disconnect=true <image> <container_id>
ctr task start <container_id>
ctr task kill <container_id>
ctr task del <container_id>
```

## dev guide


## plans

* rewrite mica-image-builder, which is just a vibe coding demo artifact
> dirty logics
> not graceful and safe implementation
* yocto
* integrate into mica library deeper
> more general pedestal interfaces
> shared filesystem, from Linux to RTOS
> snapshot design, I have some experimental ideas:
* 1. mock RTOS overlayfs
* 2. maintain a layer modification records, apply a warm patch about it to RTOS


## micrun implementations

### architecture

this minimal preview version, I remain the codes struct simple and modular:

```
- container engine

++ shim
|--> shim lifecycle, New, StartShim, Cleanup, ...
|--> shim task services, container, sandbox, pod container: Start, Kill, Delete, ...
|--> shim io, binaryIO, fileIO, pipeIO
|--> shim utils
++ runtime core
++ libs
++++ libmica
++++ pedestal

- mica
```

### shim
#### shimIO

why and how we handle IO in shim package

#### sandbox

why maintain a sandbox struct, when containerd SandboxAPI is not enabled?
> the question is the answer: maintaining an Infra (like pause container) container is a workaround
> migrate to containerd SandboxAPI is the future,
> and manager single container, pod container inside sandbox is not troublesome, so we did it.


### mount


generally, *mounting* is setup during Task Create, type of which defined as below:

```go
// containerd 1.7.x
type Mount struct {
	// Type defines the nature of the mount.
	Type string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	// Source specifies the name of the mount. Depending on mount type, this
	// may be a volume name or a host path, or even ignored.
	Source string `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	// Target path in container
	Target string `protobuf:"bytes,3,opt,name=target,proto3" json:"target,omitempty"`
	// Options specifies zero or more fstab style mount options.
	Options []string `protobuf:"bytes,4,rep,name=options,proto3" json:"options,omitempty"`
}
```

How containerd treat mount options:
```go
// mount/mount_linux.go

// parseMountOptions takes fstab style mount options and parses them for
// use with a standard mount() syscall
func parseMountOptions(options []string) (int, []string, bool) {
	var (
		flag    int
		losetup bool
		data    []string
	)
	loopOpt := "loop"
	flags := map[string]struct {
		clear bool
		flag  int
	}{
		"async":         {true, unix.MS_SYNCHRONOUS},
		"atime":         {true, unix.MS_NOATIME},
		"bind":          {false, unix.MS_BIND},
		"defaults":      {false, 0},
		"dev":           {true, unix.MS_NODEV},
		"diratime":      {true, unix.MS_NODIRATIME},
		"dirsync":       {false, unix.MS_DIRSYNC},
		"exec":          {true, unix.MS_NOEXEC},
		"mand":          {false, unix.MS_MANDLOCK},
		"noatime":       {false, unix.MS_NOATIME},
		"nodev":         {false, unix.MS_NODEV},
		"nodiratime":    {false, unix.MS_NODIRATIME},
		"noexec":        {false, unix.MS_NOEXEC},
		"nomand":        {true, unix.MS_MANDLOCK},
		"norelatime":    {true, unix.MS_RELATIME},
		"nostrictatime": {true, unix.MS_STRICTATIME},
		"nosuid":        {false, unix.MS_NOSUID},
		"rbind":         {false, unix.MS_BIND | unix.MS_REC},
		"relatime":      {false, unix.MS_RELATIME},
		"remount":       {false, unix.MS_REMOUNT},
		"ro":            {false, unix.MS_RDONLY},
		"rw":            {true, unix.MS_RDONLY},
		"strictatime":   {false, unix.MS_STRICTATIME},
		"suid":          {true, unix.MS_NOSUID},
		"sync":          {false, unix.MS_SYNCHRONOUS},
	}
	for _, o := range options {
		// If the option does not exist in the flags table or the flag
		// is not supported on the platform,
		// then it is a data value for a specific fs type
		// flags join combinination......
	}
	return flag, data, losetup
}
```

where to find supported mount type?

> spreaded in containerd source codes! 
> e.g. `snapshots/native/native_default.go` declared that `const mountType = "bind"`

take another look:

```golang
// os: linux
// oci/mounts.go

func defaultMounts() []specs.Mount {
	return []specs.Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
		},
		{
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Source:      "shm",
			Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"},
		},
		{
			Destination: "/dev/mqueue",
			Type:        "mqueue",
			Source:      "mqueue",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/run",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
	}
}

```

---

How to do for RTOS: zephyr, uniproton?

An approach is to  maintain a filesystem definitions in containerd source: bad practice


### Snapshot
