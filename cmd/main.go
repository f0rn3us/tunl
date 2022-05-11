package main

import (
	"log"
	"os"

	"github.com/f0rn3us/tunl/cmd/commands"
	"github.com/f0rn3us/tunl/pkg/version"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:                 "tunl",
		HelpName:             "tunl",
		Version:              version.String(),
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:   "host",
				Value:  "https://_.tunl.es",
				Hidden: true,
			},
		},
		Usage: "public addresses for local services",
		Commands: []*cli.Command{
			commands.DockerCommand,
			commands.DaemonCommand,
			commands.DirCommand,
			commands.FilesCommand,
			commands.HttpCommand,
			commands.TcpCommand,
			commands.VersionCommand,
			commands.WebdavCommand,
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
