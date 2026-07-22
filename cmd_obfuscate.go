package main

import (
	goflag "flag"

	"github.com/openshift/must-gather-operator/pkg/obfuscate"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

func newObfuscateCmd() *cobra.Command {
	var inputPath, outputPath, configPath string

	cmd := &cobra.Command{
		Use:   "obfuscate",
		Short: "Obfuscate a must-gather directory",
		Long:  "Run must-gather-clean against a collected must-gather tree and write cleaned output.",
		RunE: func(_ *cobra.Command, _ []string) error {
			defer klog.Flush()
			return obfuscate.RunObfuscate(inputPath, outputPath, configPath)
		},
	}

	cmd.Flags().StringVar(&inputPath, "input", "", "Path to the must-gather input directory")
	cmd.Flags().StringVar(&outputPath, "output", "", "Path to write obfuscated output")
	cmd.Flags().StringVar(&configPath, "config", obfuscate.DefaultObfuscateConfigPath, "Path to obfuscation config YAML")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")

	klogFlags := goflag.NewFlagSet("klog", goflag.ContinueOnError)
	klog.InitFlags(klogFlags)
	cmd.Flags().AddGoFlagSet(klogFlags)

	return cmd
}
