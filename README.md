# docker-9p

9P plugin for Docker

## How to build

Assuming you have Docker set up:

```
$ make rootfs create enable
```

This will create and enable a plugin called progrium/docker-9p.

## How to test

You need to have a 9P server running, like the one in [progrium/go-p9p](https://github.com/progrium/go-p9p), and then you can create
a volume pointing to it:

```
docker volume create -d progrium/docker-9p -o host=<your host> -o port=5640 testvol
```
And now you can run a container that will connect to that 9P server and have
the filesystem already mounted:

```
docker run -it -v testvol:/mnt alpine sh
```
