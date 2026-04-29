package onboard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type ItermHotkeyConfigurer func(key, command string) error

func MaybeConfigureIterm(key, clipctlBin string, stdin io.Reader, stdout io.Writer, configure ItermHotkeyConfigurer) (bool, error) {
	if stdin == nil || stdout == nil || configure == nil {
		return false, nil
	}
	fmt.Fprint(stdout, "Configure iTerm hotkey for clipctl paste? [Y/n] ")
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	if err == io.EOF && strings.TrimSpace(answer) == "" {
		return false, nil
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
		return false, nil
	}
	if err := configure(key, clipctlBin+" paste"); err != nil {
		return false, err
	}
	return true, nil
}
