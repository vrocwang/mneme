//go:build windows

package monitor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const defaultConPTYCols = 120
const defaultConPTYRows = 40

// ptyProc holds the result of starting a command with a pseudo-terminal.
type ptyProc struct {
	out     io.ReadCloser // combined stdout/stderr
	cleanup func()        // releases PTY resources
	wait    func() error  // waits for command to exit
	kill    func()        // forcefully terminates the command
}

// startPTY starts cmd with a Windows pseudo console (ConPTY).
func startPTY(cmd *exec.Cmd) (*ptyProc, error) {
	// Pipe layout:
	//   inW → ConPTY "in"  → process stdout → we read from inR
	//   outR → ConPTY "out" → process stdin  → we discarded outW
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("ConPTY in pipe: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, fmt.Errorf("ConPTY out pipe: %w", err)
	}

	size := windows.Coord{X: defaultConPTYCols, Y: defaultConPTYRows}
	var console windows.Handle
	if err := windows.CreatePseudoConsole(
		size,
		windows.Handle(inW.Fd()),  // process stdout → inW
		windows.Handle(outR.Fd()), // process stdin ← outR
		0,
		&console,
	); err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(console)
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("ProcThreadAttributeList: %w", err)
	}
	if err := attrList.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(console),
		unsafe.Sizeof(console),
	); err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(console)
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("Update ConPTY attribute: %w", err)
	}

	argv0, _ := exec.LookPath(cmd.Path)
	if argv0 == "" {
		argv0 = cmd.Path
	}
	argv0p, _ := syscall.UTF16PtrFromString(argv0)
	cmdLine := makeCmdLine(cmd.Path, cmd.Args[1:])
	cmdLinep, _ := syscall.UTF16PtrFromString(cmdLine)
	dirp, _ := syscall.UTF16PtrFromString(cmd.Dir)

	var envBlock *uint16
	if len(cmd.Env) > 0 {
		envBlock = buildEnvBlock(cmd.Env)
	}

	si := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  windows.Handle(outW.Fd()),
			StdOutput: windows.Handle(inW.Fd()),
			StdErr:    windows.Handle(inW.Fd()),
		},
		ProcThreadAttributeList: attrList.List(),
	}

	var pi windows.ProcessInformation
	err = windows.CreateProcess(
		argv0p, cmdLinep,
		nil, nil,
		true,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT,
		envBlock, dirp,
		&si.StartupInfo, &pi,
	)
	if err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(console)
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("CreateProcess: %w", err)
	}

	// Close the child-owned pipe ends so our io.Copy sees EOF on exit.
	outW.Close()
	outR.Close()
	inW.Close()

	procHandle := pi.Process
	cleanup := func() {
		if procHandle != 0 && procHandle != windows.InvalidHandle {
			windows.CloseHandle(procHandle)
		}
		if pi.Thread != 0 && pi.Thread != windows.InvalidHandle {
			windows.CloseHandle(pi.Thread)
		}
		attrList.Delete()
		windows.ClosePseudoConsole(console)
	}

	wait := func() error {
		windows.WaitForSingleObject(procHandle, windows.INFINITE)
		var exitCode uint32
		windows.GetExitCodeProcess(procHandle, &exitCode)
		if exitCode != 0 {
			return fmt.Errorf("exit status %d", exitCode)
		}
		return nil
	}

	kill := func() {
		windows.TerminateProcess(procHandle, 1)
	}

	// Wire up cmd.Process so the timeout path's cmd.Process.Kill() fallback
	// has at least something — though kill() above is the authoritative path.
	cmd.Process, _ = os.FindProcess(int(pi.ProcessId))

	return &ptyProc{
		out:     inR,
		cleanup: cleanup,
		wait:    wait,
		kill:    kill,
	}, nil
}

func makeCmdLine(path string, args []string) string {
	all := append([]string{path}, args...)
	line := ""
	for i, a := range all {
		if i > 0 {
			line += " "
		}
		line += windows.EscapeArg(a)
	}
	return line
}

func buildEnvBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	block := ""
	for _, e := range env {
		block += e + "\x00"
	}
	block += "\x00"
	p, _ := syscall.UTF16PtrFromString(block)
	return p
}
