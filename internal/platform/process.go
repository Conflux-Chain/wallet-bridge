package platform

import (
	"io"
	"os"
	"os/exec"
)

type ProcessResult struct {
	Err        error
	Diagnostic string
}

type BackgroundProcess struct {
	Done       <-chan ProcessResult
	diagnostic *os.File
}

// CloseDiagnostics detaches the caller without terminating the service.
func (p *BackgroundProcess) CloseDiagnostics() {
	if p.diagnostic != nil {
		p.diagnostic.Close()
	}
}

func StartBackground(executable, dir string, env ...string) (*BackgroundProcess, error) {
	cmd := exec.Command(executable, "service", "run", "--data-dir", dir)
	if len(env) != 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := detachProcess(cmd); err != nil {
		return nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// Nil stdin/stdout use the null device. The child never inherits MCP STDIO.
	cmd.Stderr = writer
	err = cmd.Start()
	writer.Close()
	if err != nil {
		reader.Close()
		return nil, err
	}
	output := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(reader, 4096))
		reader.Close()
		output <- string(data)
	}()
	done := make(chan ProcessResult, 1)
	go func() {
		err := cmd.Wait()
		done <- ProcessResult{Err: err, Diagnostic: <-output}
		close(done)
	}()
	return &BackgroundProcess{Done: done, diagnostic: reader}, nil
}
