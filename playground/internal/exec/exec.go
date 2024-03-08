package exec

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Cmd struct {
	*exec.Cmd
	AuditWriter io.Writer
}

func CommandContext(ctx context.Context, name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.CommandContext(ctx, name, args...)}
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	// write the command to the audit writer
	if c.AuditWriter != nil {
		o := []string{}
		if len(c.Cmd.Env) > 0 {
			o = append(o, c.Cmd.Environ()...)
		}
		o = append(o, c.Cmd.Args...)
		if _, err := c.AuditWriter.Write([]byte("==========================\n" + strings.Join(o, " ") + "\n")); err != nil {
			return nil, fmt.Errorf("error writing to audit writer: %w", err)
		}
	}

	// run the command
	out, err := c.Cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error running command: %w: out: %s", err, out)
	}

	// write the output to the audit writer
	if c.AuditWriter != nil {
		o := out
		suffix := []byte("==========================\n\n")
		if !strings.HasSuffix(string(o), "\n") {
			suffix = []byte("\n==========================\n\n")
		}
		o = append(o, suffix...)
		if _, err := c.AuditWriter.Write(o); err != nil {
			return nil, fmt.Errorf("error writing to audit writer: %w", err)
		}
	}

	return out, nil
}
