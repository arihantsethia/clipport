package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/doctor"
	"github.com/arihantsethia/clipport/internal/onboard"
	"github.com/arihantsethia/clipport/internal/shims"
	"github.com/arihantsethia/clipport/internal/shimsetup"
	"github.com/arihantsethia/clipport/internal/sshsetup"
	"github.com/arihantsethia/clipport/internal/testpaste"
	"github.com/arihantsethia/clipport/internal/token"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

func main() {
	socketPath := flag.String("socket", daemon.DefaultSocketPath(), "unix socket path")
	debug := flag.Bool("debug", false, "print detailed paste diagnostics")
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	cmd := flag.Arg(0)
	switch cmd {
	case "paste":
		resp, err := daemon.Send(*socketPath, daemon.Request{Command: "paste", SessionKey: sessionKeyFromEnv()})
		if err != nil {
			fmt.Fprintln(os.Stderr, pasteErrorMessage(resp, err, *debug))
			os.Exit(1)
		}
		if output := pasteOutput(resp); output != "" {
			fmt.Print(output)
		}
	case "session":
		if flag.NArg() < 2 || flag.Arg(1) != "register" {
			fmt.Fprintln(os.Stderr, "usage: clipport [--socket path] session register --machine name [--session-key key] [--ssh-alias alias] [--ssh-host host] [--ssh-port port] [--ssh-user user]")
			os.Exit(2)
		}
		registerSession(*socketPath, flag.Args()[2:], false)
	case "register-session":
		registerSession(*socketPath, flag.Args()[1:], true)
	case "status":
		resp, err := daemon.Send(*socketPath, daemon.Request{Command: "status"})
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("hosts: %v\n", resp.Status.ConfigHosts)
		fmt.Printf("registered sessions: %d\n", resp.Status.Registered)
		for _, b := range resp.Status.RecentBindings {
			fmt.Printf("binding %s %s", b.CreatedAt, b.Machine)
			if b.SSHAlias != "" {
				fmt.Printf(" alias=%s", b.SSHAlias)
			}
			if b.SSHHost != "" {
				fmt.Printf(" host=%s", b.SSHHost)
			}
			if b.SSHPort != "" {
				fmt.Printf(" port=%s", b.SSHPort)
			}
			if b.SSHUser != "" {
				fmt.Printf(" user=%s", b.SSHUser)
			}
			fmt.Println()
		}
		for _, t := range resp.Status.Recent {
			fmt.Printf("%s %s/%s %d bytes %s\n", t.CreatedAt, t.Host, t.Route, t.Bytes, t.Path)
		}
	case "doctor":
		doctorCmd := flag.NewFlagSet("doctor", flag.ExitOnError)
		configPath := doctorCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = doctorCmd.Parse(flag.Args()[1:])
		doctor.Print(doctor.Run(*configPath, *socketPath))
	case "test-paste":
		testCmd := flag.NewFlagSet("test-paste", flag.ExitOnError)
		configPath := testCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		hostName := testCmd.String("host", "", "logical host name")
		_ = testCmd.Parse(flag.Args()[1:])
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		result, err := testpaste.Run(cfg, *hostName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		testpaste.Print(result)
	case "ssh":
		handleSSHCommand(flag.Args()[1:])
	case "shims":
		if flag.NArg() < 2 {
			shimsUsage()
			os.Exit(2)
		}
		switch flag.Arg(1) {
		case "install":
			shimCmd := flag.NewFlagSet("shims install", flag.ExitOnError)
			target := shimCmd.String("target", "", "SSH target")
			tokenPath := shimCmd.String("token", token.DefaultPath(), "token path")
			port := shimCmd.Int("port", installedRemotePort(), "remote forward port")
			_ = shimCmd.Parse(flag.Args()[2:])
			bearer, err := token.LoadOrCreate(*tokenPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
				os.Exit(1)
			}
			if err := shims.Install(*target, bearer, *port); err != nil {
				fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("installed shims on %s\n", *target)
		case "setup":
			setupCmd := flag.NewFlagSet("shims setup", flag.ExitOnError)
			host := setupCmd.String("host", "", "logical host name")
			configPath := setupCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
			sshConfig := setupCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
			tokenPath := setupCmd.String("token", token.DefaultPath(), "token path")
			port := setupCmd.Int("port", installedRemotePort(), "remote forward port")
			_ = setupCmd.Parse(flag.Args()[2:])
			result, err := shimsetup.Setup(*configPath, *host, *sshConfig, *tokenPath, *port)
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("machine %s\n", result.Machine)
			for _, route := range result.Routes {
				fmt.Printf("- %s / %s: forward %s; shims installed\n", route.Name, route.Target, route.ForwardStatus)
			}
		case "uninstall":
			uninstallCmd := flag.NewFlagSet("shims uninstall", flag.ExitOnError)
			host := uninstallCmd.String("host", "", "logical host name")
			configPath := uninstallCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
			sshConfig := uninstallCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
			removeRemoteToken := uninstallCmd.Bool("remove-remote-token", false, "delete remote ~/.config/clipport/token")
			_ = uninstallCmd.Parse(flag.Args()[2:])
			result, err := shimsetup.Uninstall(*configPath, *host, *sshConfig, *removeRemoteToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("machine %s\n", result.Machine)
			for _, route := range result.Routes {
				fmt.Printf("- %s / %s: forward %s; shims removed\n", route.Name, route.Target, route.ForwardStatus)
			}
		default:
			shimsUsage()
			os.Exit(2)
		}
	case "uninstall":
		uninstallCmd := flag.NewFlagSet("uninstall", flag.ExitOnError)
		binDir := uninstallCmd.String("bin-dir", "", "directory containing clipport binaries; defaults to current executable directory")
		sshConfig := uninstallCmd.String("ssh-config", "", "ssh config path")
		manifestPath := uninstallCmd.String("manifest", uninstall.DefaultManifestPath(), "install manifest path")
		removeData := uninstallCmd.Bool("remove-data", false, "delete local config, cache, token, and temp files")
		keepIterm := uninstallCmd.Bool("keep-iterm", false, "leave matching clipport iTerm hotkey in place")
		dryRun := uninstallCmd.Bool("dry-run", false, "print planned actions without changing files")
		_ = uninstallCmd.Parse(flag.Args()[1:])
		keepItermSet := false
		uninstallCmd.Visit(func(f *flag.Flag) {
			if f.Name == "keep-iterm" {
				keepItermSet = true
			}
		})
		result, err := uninstall.Run(uninstall.Options{
			BinDir:         *binDir,
			SSHConfig:      *sshConfig,
			ManifestPath:   *manifestPath,
			RemoveData:     *removeData,
			RemoveIterm:    !*keepIterm,
			RemoveItermSet: keepItermSet,
			DryRun:         *dryRun,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		for _, action := range result.Actions {
			fmt.Println(action)
		}
	case "onboard":
		onboardCmd := flag.NewFlagSet("onboard", flag.ExitOnError)
		sshConfig := onboardCmd.String("ssh-config", onboard.DefaultSSHConfigPath(), "ssh config path")
		output := onboardCmd.String("output", onboard.DefaultConfigPath(), "clipport config path")
		list := onboardCmd.Bool("list", false, "list SSH hosts and exit")
		_ = onboardCmd.Parse(flag.Args()[1:])
		hosts, err := onboard.ReadSSHConfig(*sshConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		if *list {
			for _, h := range hosts {
				target := h.HostName
				if h.User != "" && target != "" {
					target = h.User + "@" + target
				}
				fmt.Printf("%-24s %s\n", h.Alias, target)
			}
			return
		}
		result, err := onboard.RunTUI(hosts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		cfg, err := onboard.BuildConfig(result.Groups, hosts, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		if err := onboard.WriteConfig(*output, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "clipport: wrote %s\n", *output)
	default:
		usage()
		os.Exit(2)
	}
}

func installedRemotePort() int {
	if manifest, err := uninstall.LoadManifest(""); err == nil {
		if port := portFromAddr(manifest.HTTPAddr); port != 0 {
			return port
		}
	}
	return sshsetup.DefaultRemotePort
}

func portFromAddr(addr string) int {
	if addr == "" {
		return 0
	}
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: clipport [--socket path] [--debug] paste")
	fmt.Fprintln(os.Stderr, "       clipport [--socket path] register-session --host name")
	fmt.Fprintln(os.Stderr, "       clipport [--socket path] session register --machine name [--session-key key] [--ssh-alias alias] [--ssh-host host] [--ssh-port port] [--ssh-user user]")
	fmt.Fprintln(os.Stderr, "       clipport [--socket path] status")
	fmt.Fprintln(os.Stderr, "       clipport [--socket path] doctor [--config path]")
	fmt.Fprintln(os.Stderr, "       clipport test-paste [--config path] [--host name]")
	fmt.Fprintln(os.Stderr, "       clipport uninstall [--manifest path] [--bin-dir path] [--ssh-config path] [--keep-iterm] [--remove-data] [--dry-run]")
	fmt.Fprintln(os.Stderr, "       clipport ssh install-forward --host alias [--ssh-config path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport ssh install-session-hook --host alias --machine name [--ssh-config path] [--clipport-bin path]")
	fmt.Fprintln(os.Stderr, "       clipport ssh install-session-hooks [--config path] [--ssh-config path] [--clipport-bin path]")
	fmt.Fprintln(os.Stderr, "       clipport shims install --target ssh-alias [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport shims setup --host machine [--config path] [--ssh-config path] [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport shims uninstall --host machine [--config path] [--ssh-config path] [--remove-remote-token]")
	fmt.Fprintln(os.Stderr, "       clipport onboard [--ssh-config path] [--output path] [--list]")
}

func shimsUsage() {
	fmt.Fprintln(os.Stderr, "usage: clipport shims install --target ssh-alias [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport shims setup --host machine [--config path] [--ssh-config path] [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport shims uninstall --host machine [--config path] [--ssh-config path] [--remove-remote-token]")
}

func pasteErrorMessage(resp daemon.Response, err error, debug bool) string {
	if !debug {
		if resp.Error != "" {
			return resp.Error
		}
		return daemon.PasteUnavailable
	}
	if resp.Error != "" && resp.Debug != "" {
		return fmt.Sprintf("%s: %s", resp.Error, resp.Debug)
	}
	if resp.Error != "" {
		return resp.Error
	}
	return fmt.Sprintf("%s: %v", daemon.PasteUnavailable, err)
}

func pasteOutput(resp daemon.Response) string {
	if resp.Text != "" {
		return resp.Text
	}
	return resp.Path
}

func registerSession(socketPath string, args []string, legacy bool) {
	register := flag.NewFlagSet("session register", flag.ExitOnError)
	host := register.String("host", "", "logical host name")
	machine := register.String("machine", "", "logical machine name")
	sshAlias := register.String("ssh-alias", "", "SSH alias used for the session")
	sshHost := register.String("ssh-host", "", "resolved SSH host")
	sshPort := register.String("ssh-port", "", "resolved SSH port")
	sshUser := register.String("ssh-user", "", "resolved SSH user")
	sessionKey := register.String("session-key", sessionKeyFromEnv(), "terminal session key")
	_ = register.Parse(args)
	if *machine == "" {
		*machine = *host
	}
	if *machine == "" {
		if legacy {
			fmt.Fprintln(os.Stderr, "clipport: register-session requires --host")
		} else {
			fmt.Fprintln(os.Stderr, "clipport: session register requires --machine")
		}
		os.Exit(2)
	}
	_, err := daemon.Send(socketPath, daemon.Request{
		Command:    "register_session",
		Host:       *host,
		Machine:    *machine,
		SessionKey: *sessionKey,
		SSHAlias:   *sshAlias,
		SSHHost:    *sshHost,
		SSHPort:    *sshPort,
		SSHUser:    *sshUser,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
		os.Exit(1)
	}
}

func handleSSHCommand(args []string) {
	if len(args) == 0 {
		sshUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "install-forward":
		sshCmd := flag.NewFlagSet("ssh install-forward", flag.ExitOnError)
		host := sshCmd.String("host", "", "SSH host alias")
		sshConfig := sshCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
		port := sshCmd.Int("port", sshsetup.DefaultRemotePort, "remote forward port")
		_ = sshCmd.Parse(args[1:])
		backup, err := sshsetup.InstallForward(*sshConfig, *host, *port)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("backup %s\n", backup)
		fmt.Printf("added RemoteForward for %s on port %d\n", *host, *port)
	case "install-session-hook":
		sshCmd := flag.NewFlagSet("ssh install-session-hook", flag.ExitOnError)
		host := sshCmd.String("host", "", "SSH host alias")
		machine := sshCmd.String("machine", "", "logical machine name")
		sshConfig := sshCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
		clipportBin := sshCmd.String("clipport-bin", defaultClipportBin(), "absolute clipport binary path")
		_ = sshCmd.Parse(args[1:])
		backup, err := sshsetup.InstallSessionHook(*sshConfig, *host, *machine, *clipportBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		if backup != "" {
			fmt.Printf("backup %s\n", backup)
			fmt.Printf("installed session hook for %s -> %s\n", *host, *machine)
			return
		}
		fmt.Printf("session hook already present for %s -> %s\n", *host, *machine)
	case "install-session-hooks":
		sshCmd := flag.NewFlagSet("ssh install-session-hooks", flag.ExitOnError)
		configPath := sshCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		sshConfig := sshCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
		clipportBin := sshCmd.String("clipport-bin", defaultClipportBin(), "absolute clipport binary path")
		_ = sshCmd.Parse(args[1:])
		results, err := installSessionHooks(*configPath, *sshConfig, *clipportBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipport: %v\n", err)
			os.Exit(1)
		}
		for _, line := range results {
			fmt.Println(line)
		}
	default:
		sshUsage()
		os.Exit(2)
	}
}

func sshUsage() {
	fmt.Fprintln(os.Stderr, "usage: clipport ssh install-forward --host alias [--ssh-config path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipport ssh install-session-hook --host alias --machine name [--ssh-config path] [--clipport-bin path]")
	fmt.Fprintln(os.Stderr, "       clipport ssh install-session-hooks [--config path] [--ssh-config path] [--clipport-bin path]")
}

func installSessionHooks(configPath, sshConfigPath, clipportBin string) ([]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	type hook struct {
		alias   string
		machine string
	}
	var hooks []hook
	seen := map[string]string{}
	for _, host := range cfg.Hosts {
		for _, route := range host.SortedRoutes() {
			if machine, ok := seen[route.SSHTarget]; ok {
				if machine != host.Name {
					return nil, fmt.Errorf("ssh alias %q is used by multiple machines: %s, %s", route.SSHTarget, machine, host.Name)
				}
				continue
			}
			seen[route.SSHTarget] = host.Name
			hooks = append(hooks, hook{alias: route.SSHTarget, machine: host.Name})
		}
	}
	var lines []string
	for _, hook := range hooks {
		backup, err := sshsetup.InstallSessionHook(sshConfigPath, hook.alias, hook.machine, clipportBin)
		if err != nil {
			return nil, err
		}
		if backup != "" {
			lines = append(lines, fmt.Sprintf("backup %s", backup))
			lines = append(lines, fmt.Sprintf("installed session hook for %s -> %s", hook.alias, hook.machine))
			continue
		}
		lines = append(lines, fmt.Sprintf("session hook already present for %s -> %s", hook.alias, hook.machine))
	}
	return lines, nil
}

func sessionKeyFromEnv() string {
	return os.Getenv("TERM_SESSION_ID")
}

func defaultClipportBin() string {
	exe, err := os.Executable()
	if err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs
		}
		return exe
	}
	return "clipport"
}
