package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/doctor"
	"github.com/arihantsethia/clipport/internal/menu"
	"github.com/getlantern/systray"
)

//go:embed assets/icon.png
var templateIconPNG []byte

const refreshEvery = 5 * time.Second
const maxHostMenuItems = 8
const maxRecentTransferItems = 4

var activeApp *trayApp

type appRunner func()

type appPaths struct {
	configPath string
	daemonPath string
	httpAddr   string
	outLog     string
	errLog     string
}

type trayApp struct {
	paths      appPaths
	controller *menu.DaemonProcess

	statusItem  *systray.MenuItem
	detailItem  *systray.MenuItem
	hostsMenu   *systray.MenuItem
	hostItems   []*systray.MenuItem
	recent      []*systray.MenuItem
	restartItem *systray.MenuItem
	doctorItem  *systray.MenuItem
	reportItem  *systray.MenuItem
	configItem  *systray.MenuItem
	logsItem    *systray.MenuItem
	quitItem    *systray.MenuItem

	lastReport string
}

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr, func() {
		systray.Run(onReady, onExit)
	}))
}

func run(args []string, stdout, stderr io.Writer, start appRunner) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "clipport: use clipctl paste")
		return 2
	}
	lock, err := acquireAppLock()
	if err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			fmt.Fprintf(stderr, "clipport: %v\n", err)
			return 1
		}
		return 0
	}
	defer lock.Close()
	start()
	return 0
}

func acquireAppLock() (*os.File, error) {
	dir := filepath.Join(os.TempDir(), "clipport", fmt.Sprint(os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, "clipport.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func onReady() {
	systray.SetTemplateIcon(templateIconPNG, templateIconPNG)
	systray.SetTooltip("Clipport")

	app := newTrayApp()
	activeApp = app
	app.buildMenu()
	if err := app.controller.Start(); err != nil {
		app.setError(err)
	} else {
		app.refresh()
	}
	go app.loop()
}

func onExit() {
	if activeApp == nil {
		return
	}
	_ = activeApp.controller.Stop()
}

func newTrayApp() *trayApp {
	paths := loadPaths()
	return &trayApp{
		paths: paths,
		controller: &menu.DaemonProcess{
			BinPath:    paths.daemonPath,
			ConfigPath: paths.configPath,
			HTTPAddr:   paths.httpAddr,
			OutLogPath: paths.outLog,
			ErrLogPath: paths.errLog,
		},
	}
}

func loadPaths() appPaths {
	binDir := defaultBinDir()
	paths := appPaths{
		configPath: doctor.DefaultConfigPath(),
		daemonPath: filepath.Join(binDir, "clipportd"),
		httpAddr:   "127.0.0.1:18765",
		outLog:     "/tmp/clipportd.out.log",
		errLog:     "/tmp/clipportd.err.log",
	}
	if local, err := config.LoadLocalBestEffort(paths.configPath); err == nil {
		if local.BinDir != "" {
			paths.daemonPath = filepath.Join(local.BinDir, "clipportd")
		}
		if local.HTTPAddr != "" {
			paths.httpAddr = local.HTTPAddr
		}
	}
	return paths
}

func defaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}

func (a *trayApp) buildMenu() {
	a.statusItem = systray.AddMenuItem("Clipport: Checking", "Clipport daemon status")
	a.statusItem.Disable()
	a.detailItem = systray.AddMenuItem("", "Status detail")
	a.detailItem.Disable()
	a.detailItem.Hide()
	a.hostsMenu = systray.AddMenuItem("Hosts", "Configured hosts")
	for range maxHostMenuItems {
		item := a.hostsMenu.AddSubMenuItem("", "Configured host")
		item.Disable()
		item.Hide()
		a.hostItems = append(a.hostItems, item)
	}

	systray.AddSeparator()
	recentHeader := systray.AddMenuItem("Recent Transfers", "Recent transfers")
	recentHeader.Disable()
	for range maxRecentTransferItems {
		item := systray.AddMenuItem("", "Recent transfer")
		item.Disable()
		item.Hide()
		a.recent = append(a.recent, item)
	}

	systray.AddSeparator()
	a.doctorItem = systray.AddMenuItem("Run Doctor", "Run Clipport health checks")
	a.reportItem = systray.AddMenuItem("Open Doctor Report", "Open the last doctor report")
	a.reportItem.Disable()
	a.reportItem.Hide()
	a.configItem = systray.AddMenuItem("Open Config", "Open Clipport config")
	a.logsItem = systray.AddMenuItem("Open Logs", "Open Clipport daemon logs")

	systray.AddSeparator()
	a.restartItem = systray.AddMenuItem("Restart", "Restart Clipport")
	a.quitItem = systray.AddMenuItem("Quit", "Quit Clipport and stop the daemon")
}

func (a *trayApp) loop() {
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.refresh()
		case <-a.restartItem.ClickedCh:
			a.setAction("Restarting")
			if err := a.controller.Restart(); err != nil {
				a.setError(err)
			} else {
				a.refresh()
			}
		case <-a.doctorItem.ClickedCh:
			a.runDoctor()
		case <-a.reportItem.ClickedCh:
			a.open(a.lastReport)
		case <-a.configItem.ClickedCh:
			a.open(a.paths.configPath)
		case <-a.logsItem.ClickedCh:
			a.openLogs()
		case <-a.quitItem.ClickedCh:
			a.setAction("Quitting")
			systray.Quit()
			return
		}
	}
}

func (a *trayApp) refresh() {
	checker := menu.Checker{
		Status: func() (daemon.Status, error) {
			resp, err := daemon.Send(daemon.DefaultSocketPath(), daemon.Request{Command: "status"})
			return resp.Status, err
		},
	}
	summary := checker.DaemonSummary()
	a.statusItem.SetTitle(summary.Title)
	systray.SetTooltip(summary.Title)
	if summary.Detail == "" {
		a.detailItem.Hide()
	} else {
		a.detailItem.SetTitle(summary.Detail)
		a.detailItem.Show()
	}
	a.updateHosts(summary.ConfigHosts)
	a.updateRecent(summary.RecentTransfers)
	a.restartItem.Enable()
}

func (a *trayApp) updateHosts(hosts []string) {
	if len(hosts) == 0 {
		a.hostsMenu.SetTitle("Hosts (none)")
		a.hostsMenu.Disable()
		for _, item := range a.hostItems {
			item.Hide()
		}
		return
	}
	a.hostsMenu.SetTitle(fmt.Sprintf("Hosts (%d)", len(hosts)))
	a.hostsMenu.Enable()
	for i, item := range a.hostItems {
		if i >= len(hosts) {
			item.Hide()
			continue
		}
		if i == len(a.hostItems)-1 && len(hosts) > len(a.hostItems) {
			item.SetTitle(fmt.Sprintf("and %d more", len(hosts)-i))
		} else {
			item.SetTitle(hosts[i])
		}
		item.Show()
	}
}

func (a *trayApp) updateRecent(transfers []daemon.Transfer) {
	for i, item := range a.recent {
		if i >= len(transfers) {
			item.Hide()
			continue
		}
		item.SetTitle(menu.TransferLabel(transfers[i]))
		item.Show()
	}
}

func (a *trayApp) setAction(title string) {
	a.statusItem.SetTitle("Clipport: " + title)
}

func (a *trayApp) setError(err error) {
	if err == nil {
		return
	}
	a.statusItem.SetTitle("Clipport: Error")
	a.detailItem.SetTitle(err.Error())
	a.detailItem.Show()
}

func (a *trayApp) runDoctor() {
	checks := doctor.Run(a.paths.configPath, daemon.DefaultSocketPath())
	report := formatDoctor(checks)
	path, err := writeDoctorReport(report)
	if err != nil {
		a.setError(err)
		return
	}
	a.lastReport = path
	failures := 0
	for _, check := range checks {
		if !check.OK {
			failures++
		}
	}
	if failures == 0 {
		a.doctorItem.SetTitle("Doctor: all checks passed")
	} else {
		a.doctorItem.SetTitle(fmt.Sprintf("Doctor: %d failed", failures))
	}
	a.reportItem.Enable()
	a.reportItem.Show()
	a.open(path)
}

func formatDoctor(checks []doctor.Check) string {
	var b strings.Builder
	for _, check := range checks {
		mark := "ok"
		if !check.OK {
			mark = "fail"
		}
		if check.Detail == "" {
			fmt.Fprintf(&b, "%-4s %s\n", mark, check.Name)
		} else {
			fmt.Fprintf(&b, "%-4s %-18s %s\n", mark, check.Name, check.Detail)
		}
	}
	return b.String()
}

func writeDoctorReport(report string) (string, error) {
	file, err := os.CreateTemp("", "clipport-doctor-*.txt")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(report); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func (a *trayApp) openLogs() {
	if fileExists(a.paths.errLog) {
		a.open(a.paths.errLog)
		return
	}
	if fileExists(a.paths.outLog) {
		a.open(a.paths.outLog)
		return
	}
	a.open(filepath.Dir(a.paths.errLog))
}

func (a *trayApp) open(path string) {
	if path == "" {
		return
	}
	if err := exec.Command("open", path).Run(); err != nil {
		a.setError(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func init() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "clipport is only supported on macOS, got %s\n", runtime.GOOS)
		os.Exit(1)
	}
}
