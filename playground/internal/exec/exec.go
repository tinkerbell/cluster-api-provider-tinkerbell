package exec

import (
	"context"
	"io"
	"log"
	"os/exec"
	"strings"
)

type Cmd struct {
	*exec.Cmd
	AuditWriter  io.Writer
	OutputWriter io.Writer
}

func CommandContext(ctx context.Context, name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.CommandContext(ctx, name, args...)}
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.AuditWriter != nil {
		var formatted []string
		if len(c.Cmd.Env) > 0 {
			formatted = append(formatted, c.Cmd.Environ()...)
		}
		formatted = append(formatted, c.Cmd.Args...)
		formatted = append(formatted, "\n")
		if _, err := c.AuditWriter.Write([]byte(strings.Join(formatted, " "))); err != nil {
			log.Printf("error writing to audit writer: %s", err)
		}
	}

	out, err := c.Cmd.CombinedOutput()
	if c.OutputWriter != nil {
		if _, err := c.OutputWriter.Write(out); err != nil {
			log.Printf("error writing to output writer: %v", err)
		}
	}

	return out, err
}
