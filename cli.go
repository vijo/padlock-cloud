package main

import "os"
import "log"
import "fmt"
import "net/http"
import "os/signal"
import "path/filepath"
import "io/ioutil"
import "gopkg.in/yaml.v2"
import "gopkg.in/urfave/cli.v1"

var gopath = os.Getenv("GOPATH")

func loadConfigFromFile(cliApp *CliApp) error {
	// load config file
	yamlData, err := ioutil.ReadFile(cliApp.ConfigPath)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlData, cliApp.Config)
	if err != nil {
		return err
	}

	return nil
}

type CliConfig struct {
	Server  AppConfig     `yaml:"server"`
	LevelDB LevelDBConfig `yaml:"leveldb"`
	Email   EmailConfig   `yaml:"email"`
}

type CliApp struct {
	*cli.App
	Config     *CliConfig
	ConfigPath string
}

func (cliApp *CliApp) RunServer(context *cli.Context) error {
	var err error

	// Load templates from assets directory
	templates, err := LoadTemplates(filepath.Join(cliApp.Config.Server.AssetsPath, "templates"))

	if err != nil {
		log.Printf("Failed to load Template! Did you specify the correct assets path? (Currently \"%s\")",
			cliApp.Config.Server.AssetsPath)
		return err
	}

	// Initialize app instance
	app, err := NewApp(
		&LevelDBStorage{LevelDBConfig: cliApp.Config.LevelDB},
		&EmailSender{cliApp.Config.Email},
		templates,
		cliApp.Config.Server,
	)

	if err != nil {
		return err
	}

	// Add rate limiting middleWare
	handler := RateLimit(app, map[Route]RateQuota{
		Route{"POST", "/auth/"}:    RateQuota{PerMin(1), 0},
		Route{"PUT", "/auth/"}:     RateQuota{PerMin(1), 0},
		Route{"DELETE", "/store/"}: RateQuota{PerMin(1), 0},
	})

	// Add CORS middleware
	handler = Cors(handler)

	// Clean up after method returns (should never happen under normal circumstances but you never know)
	defer app.CleanUp()

	// Handle INTERRUPT and KILL signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	go func() {
		s := <-c
		log.Printf("Received %v signal. Exiting...", s)
		app.CleanUp()
		os.Exit(0)
	}()

	// Start server
	log.Printf("Starting server on port %v", cliApp.Config.Server.Port)
	err = http.ListenAndServe(fmt.Sprintf(":%d", cliApp.Config.Server.Port), handler)
	if err != nil {
		return err
	}

	return nil
}

func NewCliApp() *CliApp {
	config := CliConfig{}
	cliApp := &CliApp{
		App:    cli.NewApp(),
		Config: &config,
	}
	cliApp.Name = "padlock-cloud"
	cliApp.Usage = "A command line interface for Padlock Cloud"

	cliApp.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "config, c",
			Value:       "",
			Usage:       "Path to configuration file",
			EnvVar:      "PC_CONFIG_PATH",
			Destination: &cliApp.ConfigPath,
		},
		cli.StringFlag{
			Name:        "db-path",
			Value:       "",
			Usage:       "Path to LevelDB database",
			EnvVar:      "PC_LEVELDB_PATH",
			Destination: &config.LevelDB.Path,
		},
		cli.StringFlag{
			Name:        "email-server",
			Value:       "",
			Usage:       "Mail server for sending emails",
			EnvVar:      "PC_EMAIL_SERVER",
			Destination: &config.Email.Server,
		},
		cli.StringFlag{
			Name:        "email-port",
			Value:       "",
			Usage:       "Port to use with mail server",
			EnvVar:      "PC_EMAIL_PORT",
			Destination: &config.Email.Port,
		},
		cli.StringFlag{
			Name:        "email-user",
			Value:       "",
			Usage:       "Username for authentication with mail server",
			EnvVar:      "PC_EMAIL_USER",
			Destination: &config.Email.User,
		},
		cli.StringFlag{
			Name:        "email-password",
			Value:       "",
			Usage:       "Password for authentication with mail server",
			EnvVar:      "PC_EMAIL_PASSWORD",
			Destination: &config.Email.Password,
		},
	}

	cliApp.Commands = []cli.Command{
		{
			Name:  "runserver",
			Usage: "Starts a Padlock Cloud server instance",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:        "port, p",
					Usage:       "Port to listen on",
					Value:       3000,
					EnvVar:      "PC_PORT",
					Destination: &config.Server.Port,
				},
				cli.StringFlag{
					Name:        "assets-path",
					Usage:       "Path to assets directory",
					Value:       filepath.Join(gopath, "src/github.com/maklesoft/padlock-cloud/assets"),
					EnvVar:      "PC_ASSETS_PATH",
					Destination: &config.Server.AssetsPath,
				},
				cli.BoolFlag{
					Name:        "require-tls",
					Usage:       "Reject insecure connections",
					EnvVar:      "PC_REQUIRE_TLS",
					Destination: &config.Server.RequireTLS,
				},
				cli.StringFlag{
					Name:        "notify-email",
					Usage:       "Email address to send error reports to",
					Value:       "",
					EnvVar:      "PC_NOTIFY_EMAIL",
					Destination: &config.Server.NotifyEmail,
				},
			},
			Action: cliApp.RunServer,
		},
	}

	cliApp.Before = func(context *cli.Context) error {
		if cliApp.ConfigPath != "" {
			absPath, _ := filepath.Abs(cliApp.ConfigPath)
			log.Printf("Loading config from %s - all other flags and environment variables will be ignored!", absPath)
			// Replace original config object to prevent other flags from being applied
			cliApp.Config = &CliConfig{}
			return loadConfigFromFile(cliApp)
		}
		return nil
	}

	return cliApp
}
