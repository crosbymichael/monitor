package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"

	"github.com/containerd/console"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/crosbymichael/monitor"
)

func main() {
	if err := runHtop(); err != nil {
		log.Fatal(err)
	}
}

func runHtop() error {
	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "opts")
	image, err := client.Pull(ctx, "docker.io/crosbymichael/htop:latest", containerd.WithPullUnpack)
	if err != nil {
		return err
	}

	spec, err := containerd.GenerateSpec(containerd.WithImageConfig(ctx, image), monitor.WithHtop)
	if err != nil {
		return err
	}

	container, err := client.NewContainer(
		ctx,
		"htop",
		containerd.WithSpec(spec),
		containerd.WithImage(image),
		containerd.WithNewSnapshot("htop-snapshot", image),
	)
	if err != nil {
		return err
	}
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	con := console.Current()
	defer con.Reset()
	if err := con.SetRaw(); err != nil {
		return err
	}

	task, err := container.NewTask(ctx, containerd.StdioTerminal)
	if err != nil {
		return err
	}
	defer task.Delete(ctx, containerd.WithProcessKill)

	exitStatusC := make(chan uint32, 1)
	go func() {
		status, err := task.Wait(ctx)
		if err != nil {
			fmt.Println(err)
		}
		exitStatusC <- status
	}()

	go func() {
		resize := func() error {
			size, err := con.Size()
			if err != nil {
				return err
			}
			if err := task.Resize(ctx, uint32(size.Width), uint32(size.Height)); err != nil {
				return err
			}
			return nil
		}
		resize()
		s := make(chan os.Signal, 16)
		signal.Notify(s, unix.SIGWINCH)
		for range s {
			resize()
		}
	}()
	if err := task.Start(ctx); err != nil {
		return err
	}

	<-exitStatusC

	return nil
}
