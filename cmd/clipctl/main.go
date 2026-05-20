package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/doctor"
	"github.com/arihantsethia/clipport/internal/onboard"
	"github.com/arihantsethia/clipport/internal/shims"
	"github.com/arihantsethia/clipport/internal/shimsetup"
	"github.com/arihantsethia/clipport/internal/sshsetup"
	"github.com/arihantsethia/clipport/internal/terminal"
	"github.com/arihantsethia/clipport/internal/testpaste"
	"github.com/arihantsethia/clipport/internal/token"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

func main() {
	socketPath := flag.String("socket", daemon.DefaultSocketPath(), "unix socket path")
	debug := flag.Bool("debug", false, "print detailed paste diagnostics")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	cmd := flag.Arg(0)
	switch cmd {
	case "help":
		usage()
	case "paste":
		sessionKey := sessionKeyFromEnv()
		resp, err := daemon.Send(*socketPath, daemon.Request{Command: "paste", SessionKey: sessionKey})
		if err != nil && isUnmappedSessionFailure(resp) {
			if repaired, _ := repairUnmappedSession(*socketPath, sessionKey, config.LoadDefault, terminal.ItermProvider{}.ActiveSession, confirmRepairMachine); repaired {
				resp, err = daemon.Send(*socketPath, daemon.Request{Command: "paste", SessionKey: sessionKey})
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, pasteErrorMessage(resp, err, *debug))
			os.Exit(1)
		}
		if output := pasteOutput(resp); output != "" {
			fmt.Print(output)
		}
	case "session":
		if flag.NArg() < 2 || flag.Arg(1) != "register" {
			fmt.Fprintln(os.Stderr, "usage: clipctl [--socket path] session register --machine name [--session-key key] [--ssh-alias alias] [--ssh-host host] [--ssh-port port] [--ssh-user user]")
			os.Exit(2)
		}
		registerSession(*socketPath, flag.Args()[2:], "machine")
	case "register-session":
		registerSession(*socketPath, flag.Args()[1:], "host")
	case "status":
		resp, err := daemon.Send(*socketPath, daemon.Request{Command: "status"})
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
	case "start":
		appCmd := flag.NewFlagSet("start", flag.ExitOnError)
		configPath := appCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = appCmd.Parse(flag.Args()[1:])
		if err := startInstalledApp(config.LoadLocalBestEffort, *configPath, shellRun, os.Getuid()); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("started Clipport")
	case "stop":
		appCmd := flag.NewFlagSet("stop", flag.ExitOnError)
		configPath := appCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = appCmd.Parse(flag.Args()[1:])
		if err := stopInstalledApp(config.LoadLocalBestEffort, *configPath, shellRun, cleanupInstalledAppProcesses, os.Getuid()); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("stopped Clipport")
	case "restart":
		appCmd := flag.NewFlagSet("restart", flag.ExitOnError)
		configPath := appCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = appCmd.Parse(flag.Args()[1:])
		if err := restartInstalledApp(config.LoadLocalBestEffort, *configPath, shellRun, os.Getuid()); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("restarted Clipport")
	case "doctor":
		doctorCmd := flag.NewFlagSet("doctor", flag.ExitOnError)
		configPath := doctorCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = doctorCmd.Parse(flag.Args()[1:])
		doctor.Print(doctor.Run(*configPath, *socketPath))
	case "test-paste":
		testCmd := flag.NewFlagSet("test-paste", flag.ExitOnError)
		configPath := testCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		hostName := testCmd.String("host", "", "machine name")
		_ = testCmd.Parse(flag.Args()[1:])
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		result, err := testpaste.Run(cfg, *hostName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
				os.Exit(1)
			}
			if err := shims.Install(*target, bearer, *port); err != nil {
				fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("installed shims on %s\n", *target)
		case "setup":
			setupCmd := flag.NewFlagSet("shims setup", flag.ExitOnError)
			host := setupCmd.String("host", "", "machine name")
			configPath := setupCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
			sshConfig := setupCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
			tokenPath := setupCmd.String("token", token.DefaultPath(), "token path")
			port := setupCmd.Int("port", installedRemotePort(), "remote forward port")
			_ = setupCmd.Parse(flag.Args()[2:])
			result, err := shimsetup.Setup(*configPath, *host, *sshConfig, *tokenPath, *port)
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("machine %s\n", result.Machine)
			for _, route := range result.Routes {
				fmt.Printf("- %s / %s: forward %s; shims installed\n", route.Name, route.Target, route.ForwardStatus)
			}
		case "uninstall":
			uninstallCmd := flag.NewFlagSet("shims uninstall", flag.ExitOnError)
			host := uninstallCmd.String("host", "", "machine name")
			configPath := uninstallCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
			sshConfig := uninstallCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
			removeRemoteToken := uninstallCmd.Bool("remove-remote-token", false, "delete remote ~/.config/clipport/token")
			_ = uninstallCmd.Parse(flag.Args()[2:])
			result, err := shimsetup.Uninstall(*configPath, *host, *sshConfig, *removeRemoteToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
		configPath := uninstallCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		sshConfig := uninstallCmd.String("ssh-config", "", "ssh config path")
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
			ConfigPath:     *configPath,
			SSHConfig:      *sshConfig,
			RemoveData:     *removeData,
			RemoveIterm:    !*keepIterm,
			RemoveItermSet: keepItermSet,
			DryRun:         *dryRun,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
		if err := prepareHomebrewInstallForOnboard(*output, *sshConfig); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		result, err := onboard.RunTUI(hosts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		cfg, err := onboard.BuildConfig(result.Groups, hosts, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		if err := onboard.WriteConfig(*output, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		if err := recordInstallSettings(*output, "", *sshConfig, "", "", "", ""); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: warning: could not record SSH config path: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "clipctl: wrote %s\n", *output)
		configuredIterm, err := onboard.MaybeConfigureIterm(defaultItermKey(*output), onboard.DefaultClipctlBin(), os.Stdin, os.Stdout, configureItermHotkey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		if configuredIterm {
			if err := recordItermConfigured(*output, defaultItermKey(*output)); err != nil {
				fmt.Fprintf(os.Stderr, "clipctl: warning: could not record iTerm setup: %v\n", err)
			}
		}
		if err := onboard.MaybeInstallSessionHooks(*output, *sshConfig, onboard.DefaultClipctlBin(), os.Stdin, os.Stdout, installSessionHooks); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		if err := maybeRestartInstalledApp(config.LoadLocalBestEffort, *output, func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		}, os.Getuid()); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: warning: could not restart Clipport: %v\n", err)
		}
	case "install-record":
		installCmd := flag.NewFlagSet("install-record", flag.ExitOnError)
		configPath := installCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		binDir := installCmd.String("bin-dir", "", "directory containing clipport binaries")
		sshConfig := installCmd.String("ssh-config", "", "ssh config path")
		appLaunchdPlist := installCmd.String("app-launchd-plist", "", "launch agent plist path")
		appPath := installCmd.String("app-path", "", "Clipport app bundle path")
		httpAddr := installCmd.String("http", "", "loopback HTTP address")
		itermKey := installCmd.String("iterm-key", "", "preferred iTerm hotkey")
		_ = installCmd.Parse(flag.Args()[1:])
		if err := recordInstallSettings(*configPath, *binDir, *sshConfig, *appLaunchdPlist, *appPath, *httpAddr, *itermKey); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
	case "update":
		updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
		configPath := updateCmd.String("config", doctor.DefaultConfigPath(), "clipport config path")
		_ = updateCmd.Parse(flag.Args()[1:])
		if err := updateClipport(*configPath, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func installedRemotePort() int {
	if addr := os.Getenv("CLIPPORT_HTTP"); addr != "" {
		if port := portFromAddr(addr); port != 0 {
			return port
		}
	}
	if local, err := config.LoadLocalBestEffort(""); err == nil {
		if port := portFromAddr(local.HTTPAddr); port != 0 {
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
	fmt.Fprintln(os.Stderr, "usage: clipctl [--socket path] [--debug] paste")
	fmt.Fprintln(os.Stderr, "       clipctl [--socket path] session register --machine name [--session-key key] [--ssh-alias alias] [--ssh-host host] [--ssh-port port] [--ssh-user user]")
	fmt.Fprintln(os.Stderr, "       clipctl [--socket path] register-session --host name [--session-key key]")
	fmt.Fprintln(os.Stderr, "       clipctl [--socket path] status")
	fmt.Fprintln(os.Stderr, "       clipctl start [--config path]")
	fmt.Fprintln(os.Stderr, "       clipctl stop [--config path]")
	fmt.Fprintln(os.Stderr, "       clipctl restart [--config path]")
	fmt.Fprintln(os.Stderr, "       clipctl [--socket path] doctor [--config path]")
	fmt.Fprintln(os.Stderr, "       clipctl test-paste [--config path] [--host name]")
	fmt.Fprintln(os.Stderr, "       clipctl uninstall [--config path] [--bin-dir path] [--ssh-config path] [--keep-iterm] [--remove-data] [--dry-run]")
	fmt.Fprintln(os.Stderr, "       clipctl ssh install-forward --host alias [--ssh-config path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl ssh install-session-hook --host alias --machine name [--ssh-config path] [--clipctl-bin path]")
	fmt.Fprintln(os.Stderr, "       clipctl ssh install-session-hooks [--config path] [--ssh-config path] [--clipctl-bin path]")
	fmt.Fprintln(os.Stderr, "       clipctl shims install --target ssh-alias [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl shims setup --host machine [--config path] [--ssh-config path] [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl shims uninstall --host machine [--config path] [--ssh-config path] [--remove-remote-token]")
	fmt.Fprintln(os.Stderr, "       clipctl onboard [--ssh-config path] [--output path] [--list]")
	fmt.Fprintln(os.Stderr, "       clipctl update [--config path]")
}

func shimsUsage() {
	fmt.Fprintln(os.Stderr, "usage: clipctl shims install --target ssh-alias [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl shims setup --host machine [--config path] [--ssh-config path] [--token path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl shims uninstall --host machine [--config path] [--ssh-config path] [--remove-remote-token]")
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

func isUnmappedSessionFailure(resp daemon.Response) bool {
	return strings.Contains(resp.Debug, "failed to match active iTerm session")
}

func repairUnmappedSession(socketPath string, sessionKey string, loadConfig func() (*config.Config, error), activeSession func() (terminal.Session, error), confirm func([]config.Host) (string, bool, error)) (bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}
	session, _ := activeSession()
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = session.SessionKey
	}
	if strings.TrimSpace(sessionKey) == "" {
		return false, nil
	}
	machine, ok, err := repairMachineForDetectedHost(session.DetectedHost, cfg, confirm)
	if err != nil || !ok {
		return false, err
	}
	_, err = daemon.Send(socketPath, daemon.Request{
		Command:    "register_session",
		Machine:    machine,
		SessionKey: sessionKey,
	})
	return err == nil, err
}

func repairMachineForDetectedHost(detectedHost string, cfg *config.Config, confirm func([]config.Host) (string, bool, error)) (string, bool, error) {
	if cfg == nil || len(cfg.Hosts) == 0 {
		return "", false, nil
	}
	if host, ok := explicitHostForDetectedSession(detectedHost, cfg.Hosts); ok {
		return host.Name, true, nil
	}
	return confirm(cfg.Hosts)
}

func explicitHostForDetectedSession(detected string, hosts []config.Host) (config.Host, bool) {
	detected = strings.TrimSpace(detected)
	if detected == "" {
		return config.Host{}, false
	}
	for _, host := range hosts {
		if host.Name == detected {
			return host, true
		}
		for _, alias := range host.MatchHosts {
			if alias == detected {
				return host, true
			}
		}
	}
	return config.Host{}, false
}

func confirmRepairMachine(hosts []config.Host) (string, bool, error) {
	if len(hosts) == 1 {
		msg := fmt.Sprintf("Connect this iTerm session to %s for Clipport paste?", hosts[0].Name)
		_, err := exec.Command("osascript", "-e", fmt.Sprintf(
			`display dialog %s buttons {"Cancel", "Connect"} default button "Connect" cancel button "Cancel" with title "Clipport"`,
			appleScriptString(msg),
		)).CombinedOutput()
		if err != nil {
			return "", false, nil
		}
		return hosts[0].Name, true, nil
	}

	items := make([]string, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, appleScriptString(host.Name))
	}
	out, err := exec.Command("osascript", "-e", fmt.Sprintf(
		`choose from list {%s} with title "Clipport" with prompt %s`,
		strings.Join(items, ", "),
		appleScriptString("Connect this iTerm session to a Clipport host:"),
	)).CombinedOutput()
	if err != nil {
		return "", false, nil
	}
	machine := strings.TrimSpace(string(out))
	if machine == "" || machine == "false" {
		return "", false, nil
	}
	return machine, true, nil
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func registerSession(socketPath string, args []string, machineFlag string) {
	register := flag.NewFlagSet("session register", flag.ExitOnError)
	machine := register.String(machineFlag, "", "machine name")
	sshAlias := register.String("ssh-alias", "", "SSH alias used for the session")
	sshHost := register.String("ssh-host", "", "resolved SSH host")
	sshPort := register.String("ssh-port", "", "resolved SSH port")
	sshUser := register.String("ssh-user", "", "resolved SSH user")
	sessionKey := register.String("session-key", sessionKeyFromEnv(), "terminal session key")
	_ = register.Parse(args)
	if *machine == "" {
		fmt.Fprintf(os.Stderr, "clipctl: session register requires --%s\n", machineFlag)
		os.Exit(2)
	}
	_, err := daemon.Send(socketPath, daemon.Request{
		Command:    "register_session",
		Machine:    *machine,
		SessionKey: *sessionKey,
		SSHAlias:   *sshAlias,
		SSHHost:    *sshHost,
		SSHPort:    *sshPort,
		SSHUser:    *sshUser,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("backup %s\n", backup)
		fmt.Printf("added RemoteForward for %s on port %d\n", *host, *port)
	case "install-session-hook":
		sshCmd := flag.NewFlagSet("ssh install-session-hook", flag.ExitOnError)
		host := sshCmd.String("host", "", "SSH host alias")
		machine := sshCmd.String("machine", "", "machine name")
		sshConfig := sshCmd.String("ssh-config", sshsetup.DefaultSSHConfigPath(), "ssh config path")
		clipctlBin := sshCmd.String("clipctl-bin", defaultClipctlBin(), "absolute clipctl binary path")
		_ = sshCmd.Parse(args[1:])
		backup, err := sshsetup.InstallSessionHook(*sshConfig, *host, *machine, *clipctlBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
		clipctlBin := sshCmd.String("clipctl-bin", defaultClipctlBin(), "absolute clipctl binary path")
		_ = sshCmd.Parse(args[1:])
		results, err := installSessionHooks(*configPath, *sshConfig, *clipctlBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipctl: %v\n", err)
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
	fmt.Fprintln(os.Stderr, "usage: clipctl ssh install-forward --host alias [--ssh-config path] [--port port]")
	fmt.Fprintln(os.Stderr, "       clipctl ssh install-session-hook --host alias --machine name [--ssh-config path] [--clipctl-bin path]")
	fmt.Fprintln(os.Stderr, "       clipctl ssh install-session-hooks [--config path] [--ssh-config path] [--clipctl-bin path]")
}

func installSessionHooks(configPath, sshConfigPath, clipctlBin string) ([]string, error) {
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
		backup, err := sshsetup.InstallSessionHook(sshConfigPath, hook.alias, hook.machine, clipctlBin)
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

func defaultClipctlBin() string {
	exe, err := os.Executable()
	if err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs
		}
		return exe
	}
	return "clipctl"
}

func shellRun(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

func defaultItermKey(configPath string) string {
	if local, err := config.LoadLocalBestEffort(configPath); err == nil && local.Iterm.Key != "" {
		return local.Iterm.Key
	}
	return "0x76-0x120000"
}

func configureItermHotkey(key, commandText string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	prefs := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	if err := os.MkdirAll(filepath.Dir(prefs), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(prefs); os.IsNotExist(err) {
		if err := os.WriteFile(prefs, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict/></plist>\n"), 0o600); err != nil {
			return err
		}
	}
	_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap dict", prefs).Run()
	_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Delete :GlobalKeyMap:"+key, prefs).Run()
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+" dict", prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+":Action integer 35", prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+":Text string "+commandText, prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	return nil
}

func recordItermConfigured(configPath, key string) error {
	cfg, err := config.LoadUnvalidatedOrEmpty(configPath)
	if err != nil {
		return err
	}
	cfg.Local.Iterm.Configured = true
	if cfg.Local.Iterm.Key == "" {
		cfg.Local.Iterm.Key = key
	}
	return cfg.Save(configPath)
}

func recordInstallSettings(configPath, binDir, sshConfigPath, appLaunchdPlistPath, appPath, httpAddr, itermKey string) error {
	if httpAddr != "" && !isLoopbackHTTPAddr(httpAddr) {
		return fmt.Errorf("http address must bind to loopback, got %s", httpAddr)
	}
	cfg, err := config.LoadUnvalidatedOrEmpty(configPath)
	if err != nil {
		return err
	}
	if binDir != "" {
		cfg.Local.BinDir = binDir
	}
	if sshConfigPath != "" {
		cfg.Local.SSHConfigPath = sshConfigPath
	}
	if appLaunchdPlistPath != "" {
		cfg.Local.AppLaunchdPlistPath = appLaunchdPlistPath
	}
	if appPath != "" {
		cfg.Local.AppPath = appPath
	}
	if httpAddr != "" {
		cfg.Local.HTTPAddr = httpAddr
	}
	if itermKey != "" {
		if cfg.Local.Iterm.Key != "" && cfg.Local.Iterm.Key != itermKey {
			cfg.Local.Iterm.Configured = false
		}
		cfg.Local.Iterm.Key = itermKey
	}
	return cfg.Save(configPath)
}

func prepareHomebrewInstallForOnboard(configPath, sshConfigPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	prefix, ok := homebrewPrefixForExecutable(exe)
	if !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return recordHomebrewInstall(configPath, sshConfigPath, prefix, home)
}

func recordHomebrewInstall(configPath, sshConfigPath, prefix, home string) error {
	appLink, err := ensureUserAppLink(prefix, home)
	if err != nil {
		return err
	}
	httpAddr, err := missingHTTPAddr(configPath)
	if err != nil {
		return err
	}
	return recordInstallSettings(
		configPath,
		filepath.Join(prefix, "bin"),
		sshConfigPath,
		filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist"),
		appLink,
		httpAddr,
		"0x76-0x120000",
	)
}

func homebrewPrefixForExecutable(exe string) (string, bool) {
	resolvedExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolvedExe = exe
	}
	for _, prefix := range homebrewPrefixCandidates() {
		if !homebrewClipportFilesExist(prefix) {
			continue
		}
		clipctlPath := filepath.Join(prefix, "bin", "clipctl")
		resolvedClipctl, err := filepath.EvalSymlinks(clipctlPath)
		if err != nil {
			resolvedClipctl = clipctlPath
		}
		if resolvedExe == resolvedClipctl || exe == clipctlPath {
			return prefix, true
		}
	}
	return "", false
}

func homebrewPrefixCandidates() []string {
	seen := map[string]bool{}
	var prefixes []string
	for _, prefix := range []string{
		filepath.Join(os.Getenv("HOMEBREW_PREFIX"), "opt", "clipport"),
		"/opt/homebrew/opt/clipport",
		"/usr/local/opt/clipport",
	} {
		if prefix == "opt/clipport" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func homebrewClipportFilesExist(prefix string) bool {
	for _, path := range []string{
		filepath.Join(prefix, "bin", "clipctl"),
		filepath.Join(prefix, "libexec", "Clipport.app", "Contents", "MacOS", "clipport"),
		filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist"),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func ensureUserAppLink(prefix, home string) (string, error) {
	appTarget := filepath.Join(prefix, "libexec", "Clipport.app")
	appLink := filepath.Join(home, "Applications", "Clipport.app")
	if _, err := os.Stat(appTarget); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(appLink), 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(appLink)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(appLink); err != nil {
				return "", err
			}
		} else if existingBundleID(appLink) == "com.clipport.app" {
			if err := os.RemoveAll(appLink); err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("%s already exists and is not Clipport", appLink)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(appTarget, appLink); err != nil {
		return "", err
	}
	registerAppWithLaunchServices(appLink)
	return appLink, nil
}

func existingBundleID(appPath string) string {
	data, err := os.ReadFile(filepath.Join(appPath, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	if strings.Contains(string(data), "<string>com.clipport.app</string>") {
		return "com.clipport.app"
	}
	return ""
}

func registerAppWithLaunchServices(appPath string) {
	lsregister := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(lsregister); err != nil {
		return
	}
	_ = exec.Command(lsregister, "-f", appPath).Run()
}

func missingHTTPAddr(configPath string) (string, error) {
	local, err := config.LoadLocalBestEffort(configPath)
	if err != nil {
		return "", err
	}
	if local.HTTPAddr != "" {
		return "", nil
	}
	return freeLoopbackHTTPAddr(18765, 18865)
}

func freeLoopbackHTTPAddr(start, end int) (string, error) {
	for port := start; port <= end; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			_ = listener.Close()
			return addr, nil
		}
	}
	return "", fmt.Errorf("no free loopback HTTP port found in %d-%d", start, end)
}

func isLoopbackHTTPAddr(addr string) bool {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if _, err := strconv.Atoi(portText); err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func maybeRestartInstalledApp(loadLocal func(string) (config.LocalConfig, error), configPath string, run func(name string, args ...string) error, uid int) error {
	local, err := loadLocal(configPath)
	if err != nil {
		return nil
	}
	if local.AppLaunchdPlistPath == "" {
		return nil
	}
	return maybeStartOrRestartInstalledApp(local, launchAgentLoaded, run, cleanupInstalledAppProcesses, uid)
}

func maybeStartOrRestartInstalledApp(local config.LocalConfig, isRunning func(config.LocalConfig, int) (bool, error), run func(name string, args ...string) error, cleanup func([]string) error, uid int) error {
	running, err := isRunning(local, uid)
	if err != nil {
		return err
	}
	if running {
		return restartInstalledAppWithLocal(local, run, cleanup, uid)
	}
	return startInstalledAppWithLocal(local, run, uid)
}

func startInstalledApp(loadLocal func(string) (config.LocalConfig, error), configPath string, run func(name string, args ...string) error, uid int) error {
	local, err := resolvedLocalSettings(loadLocal, configPath)
	if err != nil {
		return err
	}
	return startInstalledAppWithLocal(local, run, uid)
}

func startInstalledAppWithLocal(local config.LocalConfig, run func(name string, args ...string) error, uid int) error {
	guiDomain := fmt.Sprintf("gui/%d", uid)
	if err := run("launchctl", "bootstrap", guiDomain, local.AppLaunchdPlistPath); err != nil && !isLaunchctlAlreadyLoaded(err) {
		return err
	}
	return run("launchctl", "kickstart", "-k", fmt.Sprintf("%s/%s", guiDomain, uninstall.AppLaunchdLabel))
}

func stopInstalledApp(loadLocal func(string) (config.LocalConfig, error), configPath string, run func(name string, args ...string) error, cleanup func([]string) error, uid int) error {
	local, err := resolvedLocalSettings(loadLocal, configPath)
	if err != nil {
		return err
	}
	guiDomain := fmt.Sprintf("gui/%d", uid)
	if err := run("launchctl", "bootout", guiDomain, local.AppLaunchdPlistPath); err != nil && !isLaunchctlNotLoaded(err) && !isMissingLaunchAgentPlist(err, local.AppLaunchdPlistPath) {
		return err
	}
	return cleanup(installedAppProcessPaths(local))
}

func restartInstalledApp(loadLocal func(string) (config.LocalConfig, error), configPath string, run func(name string, args ...string) error, uid int) error {
	local, err := resolvedLocalSettings(loadLocal, configPath)
	if err != nil {
		return err
	}
	return restartInstalledAppWithLocal(local, run, cleanupInstalledAppProcesses, uid)
}

func restartInstalledAppWithLocal(local config.LocalConfig, run func(name string, args ...string) error, cleanup func([]string) error, uid int) error {
	guiDomain := fmt.Sprintf("gui/%d", uid)
	if err := run("launchctl", "bootout", guiDomain, local.AppLaunchdPlistPath); err != nil && !isLaunchctlNotLoaded(err) && !isMissingLaunchAgentPlist(err, local.AppLaunchdPlistPath) {
		return err
	}
	if err := cleanup(installedAppProcessPaths(local)); err != nil {
		return err
	}
	if err := run("launchctl", "bootstrap", guiDomain, local.AppLaunchdPlistPath); err != nil {
		return err
	}
	return run("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, uninstall.AppLaunchdLabel))
}

func launchAgentLoaded(local config.LocalConfig, uid int) (bool, error) {
	if local.AppLaunchdPlistPath == "" {
		return false, nil
	}
	out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, uninstall.AppLaunchdLabel)).CombinedOutput()
	if err == nil {
		return true, nil
	}
	text := strings.ToLower(strings.TrimSpace(string(out)))
	if text == "" {
		text = strings.ToLower(err.Error())
	}
	if strings.Contains(text, "could not find service") || strings.Contains(text, "not found") {
		return false, nil
	}
	return false, err
}

func installedAppProcessPaths(local config.LocalConfig) []string {
	var paths []string
	if local.AppPath != "" {
		paths = append(paths, filepath.Join(local.AppPath, "Contents", "MacOS", "clipport"))
	}
	if local.BinDir != "" {
		paths = append(paths, filepath.Join(local.BinDir, "clipport"))
	}
	return paths
}

func cleanupInstalledAppProcesses(paths []string) error {
	return newAppProcessCleaner().clean(paths)
}

type appProcessCleaner struct {
	listClipportPIDs func() ([]int, error)
	executablePath   func(int) (string, error)
	signal           func(int, syscall.Signal) error
	isRunning        func(int) bool
	sleep            func(time.Duration)
	waitInterval     time.Duration
	waitTimeout      time.Duration
}

func newAppProcessCleaner() appProcessCleaner {
	return appProcessCleaner{
		listClipportPIDs: listClipportPIDs,
		executablePath:   executablePathForPID,
		signal:           signalPID,
		isRunning:        processRunning,
		sleep:            time.Sleep,
		waitInterval:     50 * time.Millisecond,
		waitTimeout:      2 * time.Second,
	}
}

func (c appProcessCleaner) clean(paths []string) error {
	wanted := map[string]bool{}
	for _, path := range paths {
		if path != "" {
			wanted[path] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	pids, err := c.listClipportPIDs()
	if err != nil {
		return err
	}
	var matched []int
	for _, pid := range pids {
		executable, err := c.executablePath(pid)
		if err != nil {
			continue
		}
		if !isInstalledAppExecutable(executable, wanted) {
			continue
		}
		if err := c.signal(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		matched = append(matched, pid)
	}
	return c.waitThenKill(matched)
}

func (c appProcessCleaner) waitThenKill(pids []int) error {
	deadline := time.Now().Add(c.waitTimeout)
	for _, pid := range pids {
		for c.isRunning(pid) && time.Now().Before(deadline) {
			c.sleep(c.waitInterval)
		}
		if c.isRunning(pid) {
			if err := c.signal(pid, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
		}
	}
	return nil
}

func listClipportPIDs() ([]int, error) {
	out, err := exec.Command("pgrep", "-x", "clipport").Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func executablePathForPID(pid int) (string, error) {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-a", "-d", "txt", "-Fn").Output()
	if err != nil {
		return "", err
	}
	path, ok := executablePathFromLsof(string(out))
	if !ok {
		return "", fmt.Errorf("executable path unavailable for pid %d", pid)
	}
	return path, nil
}

func executablePathFromLsof(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return strings.TrimPrefix(line, "n"), true
		}
	}
	return "", false
}

func signalPID(pid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(sig)
}

func processRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func isInstalledAppExecutable(executable string, wanted map[string]bool) bool {
	return wanted[executable]
}

func resolvedLocalSettings(loadLocal func(string) (config.LocalConfig, error), configPath string) (config.LocalConfig, error) {
	local, err := loadLocal(configPath)
	if err != nil {
		return config.LocalConfig{}, err
	}
	if local.BinDir == "" {
		local.BinDir = defaultBinDir()
	}
	if local.AppLaunchdPlistPath == "" {
		local.AppLaunchdPlistPath = defaultAppLaunchdPlistPath()
	}
	if local.AppPath == "" {
		local.AppPath = defaultAppPath()
	}
	return local, nil
}

func defaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}

func defaultAppLaunchdPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", uninstall.AppLaunchdLabel+".plist")
}

func defaultAppPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Applications", "Clipport.app")
}

func updateClipport(configPath string, stdout, stderr io.Writer) error {
	local, err := resolvedLocalSettings(config.LoadLocalBestEffort, configPath)
	if err != nil {
		return err
	}
	name, args, env := buildUpdateInvocation(configPath, local)
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func buildUpdateInvocation(configPath string, local config.LocalConfig) (string, []string, []string) {
	env := append([]string{}, os.Environ()...)
	if local.BinDir != "" {
		env = append(env, "CLIPPORT_BIN="+local.BinDir)
	}
	if configPath != "" {
		env = append(env, "CLIPPORT_CONFIG="+configPath)
	}
	if local.HTTPAddr != "" {
		env = append(env, "CLIPPORT_HTTP="+local.HTTPAddr)
	}
	if local.Iterm.Key != "" {
		env = append(env, "CLIPPORT_ITERM_KEY="+local.Iterm.Key)
	}
	return "sh", []string{"-c", curlInstallerScript()}, env
}

func curlInstallerScript() string {
	return `set -eu
repo_ref="${CLIPPORT_REF:-main}"
curl -fsSL "https://raw.githubusercontent.com/arihantsethia/clipport/${repo_ref}/install.sh" | sh`
}

func isLaunchctlAlreadyLoaded(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already loaded") || strings.Contains(text, "already bootstrapped")
}

func isLaunchctlNotLoaded(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not loaded") ||
		strings.Contains(text, "could not find service") ||
		strings.Contains(text, "no such process") ||
		(strings.Contains(text, "boot-out failed: 5") && strings.Contains(text, "input/output error"))
}

func isMissingLaunchAgentPlist(err error, path string) bool {
	if err == nil || path == "" {
		return false
	}
	if !strings.Contains(strings.ToLower(err.Error()), "input/output error") {
		return false
	}
	_, statErr := os.Stat(path)
	return errors.Is(statErr, os.ErrNotExist)
}
