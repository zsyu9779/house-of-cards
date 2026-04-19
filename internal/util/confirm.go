package util

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter reads a single line of user input. Defaults read from os.Stdin.
// Tests inject their own io.Reader via NewPrompter.
type Prompter struct {
	In  io.Reader
	Out io.Writer
}

// DefaultPrompter reads from stdin and writes to stdout.
func DefaultPrompter() *Prompter {
	return &Prompter{In: os.Stdin, Out: os.Stdout}
}

// NewPrompter returns a Prompter bound to the given streams.
func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: in, Out: out}
}

// ConfirmTyped asks the user to retype expected. Returns (true, nil) only on exact match.
// It does not loop — a mismatch returns (false, nil) so the caller can abort.
//
// warning is printed before the prompt; expected is the literal string the user must type.
func (p *Prompter) ConfirmTyped(warning, expected string) (bool, error) {
	if warning != "" {
		if _, err := fmt.Fprintln(p.Out, warning); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprintf(p.Out, "   输入 %q 确认（其他任意内容将取消）: ", expected); err != nil {
		return false, err
	}
	reader := bufio.NewReader(p.In)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimSpace(line) == expected, nil
}

// ConfirmYesNo asks a y/N question. Empty or non-y answers default to false.
func (p *Prompter) ConfirmYesNo(warning string) (bool, error) {
	if warning != "" {
		if _, err := fmt.Fprintln(p.Out, warning); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprint(p.Out, "   继续？[y/N]: "); err != nil {
		return false, err
	}
	reader := bufio.NewReader(p.In)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}
