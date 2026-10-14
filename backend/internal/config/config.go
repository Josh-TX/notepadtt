package config

import (
	"github.com/alecthomas/kong"
)

type Config struct {
	Directory string           `short:"d" env:"NOTEPADTT_DIRECTORY" default:"." type:"path" help:"Root directory to serve."`
	Port      int              `short:"p" env:"NOTEPADTT_PORT" default:"8080" help:"Port to listen on."`
	Version   kong.VersionFlag `short:"v" help:"Print version and exit."`
}

// Load parses args (typically os.Args[1:]); flags override env vars, which override defaults.
func Load(args []string, version string, opts ...kong.Option) (Config, error) {
	var cfg Config
	opts = append([]kong.Option{
		kong.Name("notepadtt"),
		kong.Description("Web-based notepad that serves a directory of text files."),
		kong.Vars{"version": version},
		kong.UsageOnError(),
	}, opts...)
	k, err := kong.New(&cfg, opts...)
	if err != nil {
		return cfg, err
	}
	_, err = k.Parse(args)
	return cfg, err
}
