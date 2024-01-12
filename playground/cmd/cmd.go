package cmd

import (
	"context"
	"flag"

	"github.com/peterbourgon/ff/v3/ffcli"
)

type State struct {
	// ClusterName is the name of the cluster
	ClusterName string `yaml:"clusterName"`
	// OutputDir is the directory location for all created files
	OutputDir string `yaml:"outputDir"`
	// TotalHardware is the number of hardware CR that will be created in the management cluster
	TotalHardware int `yaml:"totalHardware"`
}

type rootConfig struct {
	// StateFile is the file location of the state file. This file holds all the information about a created playground
	StateFile string
}

type Label string
type NodeRole string

const (
	// ControlPlaneRole is the label value for control plane nodes
	ControlPlaneRole NodeRole = "control-plane"
	// WorkerRole is the label value for worker nodes
	WorkerRole NodeRole = "worker"
	// CAPTRole is the label value for the role a node will be in the cluster
	CAPTRole Label = "tinkerbell.org/role"
	// ClusterName is the default name of the cluster the playground creates
	ClusterName = "playground"
)

func Execute(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("capt-playground", flag.ExitOnError)
	gf := &rootConfig{}
	create := NewCreateCommand(gf)
	delete := NewDeleteCommand(gf)
	gf.registerRootFlags(fs)
	cmd := &ffcli.Command{
		Name:        "capt-playground",
		ShortUsage:  "capt-playground [flags] <subcommand> [flags] [<arg>...]",
		ShortHelp:   "CLI for creating a CAPT playground",
		FlagSet:     fs,
		Subcommands: []*ffcli.Command{create, delete},
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
	}
	return cmd.ParseAndRun(ctx, args)
}

func (r *rootConfig) registerRootFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.StateFile, "state-file", "./state.yaml", "file location of the state file")
}
