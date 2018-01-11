package main

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

type Volume struct {
	Host string
	Port string

	Mountpoint  string
	connections int
}

type Driver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*Volume
}

func newDriver(root string) (*Driver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &Driver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "9p-state.json"),
		volumes:   map[string]*Volume{},
	}

	data, err := ioutil.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *Driver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *Driver) Create(r *volume.CreateRequest) error {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &Volume{}

	for key, val := range r.Options {
		switch key {
		case "host":
			v.Host = val
		case "port":
			v.Port = val
		default:
			return responseError(fmt.Sprintf("unknown option %q", val))
		}
	}

	if v.Host == "" {
		return responseError("'host' option required")
	}
	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.Host)))) // TODO: fixme

	d.volumes[r.Name] = v

	d.saveState()

	return nil
}

func (d *Driver) Remove(r *volume.RemoveRequest) error {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections != 0 {
		return responseError(fmt.Sprintf("volume %s is currently used by a container", r.Name))
	}
	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return responseError(err.Error())
	}
	delete(d.volumes, r.Name)
	d.saveState()
	return nil
}

func (d *Driver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return nil, responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return &volume.PathResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *Driver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return nil, responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return nil, responseError(err.Error())
			}
		} else if err != nil {
			return nil, responseError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return nil, responseError(fmt.Sprintf("%v already exist and it's not a directory", v.Mountpoint))
		}

		if err := d.mountVolume(v); err != nil {
			return nil, responseError(err.Error())
		}
	}

	v.connections++

	return &volume.MountResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *Driver) Unmount(r *volume.UnmountRequest) error {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v.Mountpoint); err != nil {
			return responseError(err.Error())
		}
		v.connections = 0
	}

	return nil
}

func (d *Driver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return nil, responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}, nil
}

func (d *Driver) List() (*volume.ListResponse, error) {
	logrus.WithField("method", "list").Debugf("list")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *Driver) Capabilities() *volume.CapabilitiesResponse {
	logrus.WithField("method", "capabilities").Debugf("capabilities")

	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *Driver) mountVolume(v *Volume) error {
	port := v.Port
	if port == "" {
		port = "564"
	}
	cmd := exec.Command("mount", "-t", "9p", v.Host, "-o", fmt.Sprintf("trans=tcp,port=%s", port), v.Mountpoint)
	logrus.Debug(cmd.Args)
	return cmd.Run()
}

func (d *Driver) unmountVolume(target string) error {
	cmd := fmt.Sprintf("umount %s", target)
	logrus.Debug(cmd)
	return exec.Command("sh", "-c", cmd).Run()
}

func responseError(err string) error {
	logrus.Error(err)
	return errors.New(err)
}

func main() {
	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		logrus.SetLevel(logrus.DebugLevel)
	}

	d, err := newDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening ...")
	logrus.Error(h.ServeUnix("9p", 0))
}
