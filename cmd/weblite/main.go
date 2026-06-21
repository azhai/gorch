package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type Options struct {
	Port int
	Dir  string
	Bind string
}

func main() {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "weblite",
		Short: "Lightweight static file server",
		RunE:  opts.runE,
	}

	cmd.Flags().IntVarP(&opts.Port, "port", "p", 8000, "listen port")
	cmd.Flags().StringVarP(&opts.Dir, "dir", "d", ".", "directory to serve")
	cmd.Flags().StringVarP(&opts.Bind, "bind", "b", "127.0.0.1", "bind address")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func (o *Options) validate() error {
	if o.Port < 1 || o.Port > 65535 {
		return fmt.Errorf("invalid port number: must be between 1 and 65535")
	}

	info, err := os.Stat(o.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory not found: %s", o.Dir)
		}
		return fmt.Errorf("directory not accessible: %s", o.Dir)
	}

	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", o.Dir)
	}

	return nil
}

func (o *Options) runE(cmd *cobra.Command, args []string) error {
	if err := o.validate(); err != nil {
		return err
	}

	srv := NewServer(o)
	return srv.Start()
}
