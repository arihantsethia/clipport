package onboard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type SessionHookInstaller func(configPath, sshConfigPath, clipctlBin string) ([]string, error)

func MaybeInstallSessionHooks(configPath, sshConfigPath, clipctlBin string, stdin io.Reader, stdout io.Writer, install SessionHookInstaller) error {
	if stdin == nil || stdout == nil || install == nil {
		return nil
	}
	fmt.Fprint(stdout, "Enable automatic SSH session matching? [Y/n] ")
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if err == io.EOF && strings.TrimSpace(answer) == "" {
		return nil
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
		return nil
	}
	lines, err := install(configPath, sshConfigPath, clipctlBin)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

func DefaultClipctlBin() string {
	exe, err := os.Executable()
	if err != nil {
		return "clipctl"
	}
	return exe
}
