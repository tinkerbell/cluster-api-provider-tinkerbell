package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
	"gopkg.in/yaml.v2"
)

type Delete struct {
	rootConfig *rootConfig
}

func NewDeleteCommand(rc *rootConfig) *ffcli.Command {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	d := &Delete{rootConfig: rc}
	rc.registerRootFlags(fs)
	return &ffcli.Command{
		Name:       "delete",
		ShortUsage: "capt-playground delete [flags]",
		ShortHelp:  "Delete the CAPT playground",
		Options:    []ff.Option{ff.WithEnvVarPrefix("CAPT_PLAYGROUND")},
		FlagSet:    fs,
		Exec: func(ctx context.Context, _ []string) error {
			return d.exec(ctx)
		},
	}
}

func (d *Delete) exec(ctx context.Context) error {
	/*
		kind delete cluster --name playground
		rm -rf output/
		docker rm -f virtualbmc
		for i in {1..4}; do echo $i; sudo virsh destroy "node$i"; sudo virsh undefine "node$i" --remove-all-storage --nvram; done
	*/
	data, err := os.ReadFile(d.rootConfig.StateFile)
	if err != nil {
		return err
	}

	s := State{}
	if err := yaml.Unmarshal([]byte(data), &s); err != nil {
		return err
	}

	// delete kind cluster
	// delete output dir
	// delete virtualbmc docker container
	// delete all virsh nodes

	log.Println("Deleting KinD cluster")
	if errC := deleteKindCluster(s.ClusterName); errC != nil {
		err = fmt.Errorf("error deleting kind cluster: %w", errC)
	}

	log.Println("Deleting output directory")
	if errC := deleteOutputDir(s.OutputDir); errC != nil {
		err = fmt.Errorf("error deleting output directory: %w", errC)
	}

	log.Println("Deleting virtualbmc docker container")
	if errC := deleteDockerContainer("virtualbmc"); errC != nil {
		err = fmt.Errorf("error deleting virtualbmc docker container: %w", errC)
	}

	log.Println("Deleting virsh nodes")
	if errC := deleteVirshNodes(s.TotalHardware); errC != nil {
		err = fmt.Errorf("error deleting virsh nodes: %w", errC)
	}

	return err
}

func deleteKindCluster(name string) error {
	/*
		kind delete cluster --name playground
	*/
	cmd := "kind"
	args := []string{"delete", "cluster", "--name", name}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error deleting kind cluster: %w: %s", err, out)
	}

	return nil
}

func deleteOutputDir(dir string) error {
	return os.RemoveAll(dir)
}

func deleteDockerContainer(name string) error {
	/*
		docker rm -f <name>
	*/
	cmd := "docker"
	args := []string{"rm", "-f", name}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error deleting docker container: %w: %s", err, out)
	}

	return nil
}

func deleteVirshNodes(num int) error {
	/*
		for i in {1..4}; do echo $i; virsh -c qemu:///system destroy "node$i"; virsh -c qemu:///system undefine "node$i" --remove-all-storage --nvram; done
	*/
	cmd := "virsh"
	for i := 1; i <= num; i++ {
		// This stops the VM, needed before the undefine command can be run successfully
		args := []string{"-c", "qemu:///system", "destroy", fmt.Sprintf("node%d", i)}
		e := exec.CommandContext(context.Background(), cmd, args...)
		out, err := e.CombinedOutput()
		if err != nil && !contains(strings.ToLower(string(out)), []string{"domain not found", "failed to get domain", "domain is not running"}) {
			// return fmt.Errorf("error destroying virsh node, command: `%v %v`, err: %w: output: %s", cmd, strings.Join(args, " "), err, out)
			continue
		}

		// remove the VM and any disks associated with it
		args = []string{"-c", "qemu:///system", "undefine", fmt.Sprintf("node%d", i), "--remove-all-storage", "--nvram"}
		e = exec.CommandContext(context.Background(), cmd, args...)
		out, err = e.CombinedOutput()
		if err != nil && !contains(strings.ToLower(string(out)), []string{"domain not found", "failed to get domain"}) {
			return fmt.Errorf("error removing virsh node: command: `%v %v`, err: %w: output: %s", cmd, strings.Join(args, " "), err, out)
		}
	}

	return nil
}

func contains(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}
