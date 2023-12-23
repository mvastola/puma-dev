package dev

import (
	"fmt"
	"github.com/creack/pty"
	"github.com/hairyhenderson/go-which"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/puma/puma-dev/dev/rpc"
	"github.com/vektra/errors"
	"golang.org/x/term"
	"gopkg.in/tomb.v2"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

var DefaultShell = "/bin/bash"

// TODO: store a map of persisted apps

type RpcConsoleProgResult struct {
	State *os.ProcessState
}

type RpcConsoleProgOpts struct {
	Key         rpc.Maybe[string]
	Dir         rpc.Maybe[string]
	Env         map[string]string
	Argv        []string
	UseShell    rpc.Maybe[bool]
	Shell       rpc.Maybe[string]
	ShellArgs   []string
	Persist     rpc.Maybe[bool]
	Interactive rpc.Maybe[bool]
	AllocPty    rpc.Maybe[bool]
	Attributes  syscall.SysProcAttr
	IdleTimeout rpc.Maybe[time.Duration]
}

type RpcConsoleProg struct {
	Key         rpc.Maybe[string]
	Label       string
	App         *App
	Command     *exec.Cmd
	Cmdline     string
	Process     *os.Process
	Result      RpcConsoleProgResult // TODO: implement this
	AllocPty    bool
	IdleTimeout rpc.Maybe[time.Duration]

	tmpDir          string
	tty             string
	pty             *os.File
	t               tomb.Tomb
	cleanupRoutines []func()

	//Events  *Events

	//lines       linebuffer.LineBuffer
	//lastLogLine string
	//t tomb.Tomb

	stdin   io.Writer
	stdout  io.Reader
	stderr  io.Reader
	lastUse rpc.Maybe[time.Time]
	lock    sync.Mutex

	readyChan chan struct{}
}

func (prog *RpcConsoleProg) ComputeShellArgs(opts *RpcConsoleProgOpts) []string {
	if !opts.UseShell.HasValue() {
		return make([]string, 0)
	}
	var ok bool
	var shellCmd string
	if opts.Env != nil {
		shellCmd, ok = opts.Env["SHELL"]
	}
	if !ok {
		shellCmd, ok = os.LookupEnv("SHELL")
	}
	if !ok {
		fmt.Printf("! SHELL env var not set, using %s by default", DefaultShell)
		shellCmd = DefaultShell
	}
	args := []string{shellCmd}
	if opts.ShellArgs != nil {
		args = append(args, opts.ShellArgs...)
	}
	if opts.Interactive.ValueOr(false) {
		args = append(args, "-l", "-i")
	}

	return args
}

func (prog *RpcConsoleProg) eventAdd(name string, extraArgs ...interface{}) {
	a := prog.App
	args := make([]interface{}, 0)
	args = append(args, "type", "console_prog")
	if prog.Process != nil {
		args = append(args, "pid", prog.Process.Pid)
	}
	prog.Key.WithValue(func(key string) any {
		args = append(args, "programKey", prog.Key)
		return nil
	}, nil)

	args = append(args, extraArgs...)
	a.eventAdd(name, args...)
}

func NewRpcConsoleProgOpts() *RpcConsoleProgOpts {
	return &RpcConsoleProgOpts{
		Key:         rpc.NewMaybe[string](),
		Dir:         rpc.NewMaybe[string](),
		UseShell:    rpc.NewMaybe[bool](),
		Shell:       rpc.NewMaybe[string](),
		Persist:     rpc.NewMaybe[bool](),
		Interactive: rpc.NewMaybe[bool](),
		AllocPty:    rpc.NewMaybe[bool](),
		IdleTimeout: rpc.NewMaybe[time.Duration](),
		Attributes:  syscall.SysProcAttr{},
	}
}
func (a *App) InitConsoleApp(opts *RpcConsoleProgOpts) (*RpcConsoleProg, error) {
	prog := &RpcConsoleProg{
		App:             a,
		Key:             opts.Key, // TODO: lookup key first
		lock:            sync.Mutex{},
		cleanupRoutines: make([]func(), 0),
		Result:          RpcConsoleProgResult{State: nil},
		IdleTimeout:     opts.IdleTimeout,
		lastUse:         rpc.NewMaybe[time.Time](),
	}
	err := prog.Init(opts)
	if err != nil {
		return nil, err
	}
	return prog, nil
}

func (prog *RpcConsoleProg) fullCmdArgs(opts *RpcConsoleProgOpts) ([]string, error) {
	var fullArgs = make([]string, 0)
	shellArgs := prog.ComputeShellArgs(opts)
	if shellArgs != nil {
		fullArgs = append(fullArgs, shellArgs...)
	}
	if opts.Argv != nil && len(opts.Argv) > 0 {
		fullArgs = append(fullArgs, "-c")
		fullArgs = append(fullArgs, opts.Argv...)
	} else if len(fullArgs) <= 0 {
		return nil, errors.New("No args given to launch non-shell program")
	}

	prog.Key.ApplyDefault(path.Base(fullArgs[0]))
	labelParts := []string{prog.App.Name}
	prog.Key.WithValue(func(key string) any {
		labelParts = append(labelParts, key)
		return nil
	})
	prog.Label = strings.Join(labelParts, "-")

	if !path.IsAbs(fullArgs[0]) {
		whichResult := which.Which(fullArgs[0])
		if len(whichResult) == 0 {
			errmsg := fmt.Sprintf("Could not find executable in PATH for command %s", fullArgs[0])
			return nil, errors.New(errmsg)
		}
		fullArgs[0] = whichResult
	}
	prog.Cmdline = shellquote.Join(fullArgs...)
	return fullArgs, nil
}

func (prog *RpcConsoleProg) Init(opts *RpcConsoleProgOpts) error {
	var a *App = prog.App

	opts.Dir.ApplyDefault(a.dir)
	opts.Persist.ApplyDefault(opts.Key.HasValue())
	if !opts.Persist.HasValue() {
		return errors.New("key not given for persistent RpcConsoleProg")
	}

	opts.UseShell.ApplyDefault(true)
	opts.AllocPty.ApplyDefault(true)

	var (
		fullArgs []string
		err      error
	)
	fullArgs, err = prog.fullCmdArgs(opts)

	if err != nil {
		errCtx := fmt.Sprintf("Generating arguments for command in console prog %s", prog.Label)
		return errors.Context(err, errCtx)
	}
	prog.Command = exec.Command(fullArgs[0], fullArgs[1:]...)

	cmd := prog.Command
	cmd.Dir = opts.Dir.ValueOr(a.dir)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &opts.Attributes

	tmpDirTemplate := fmt.Sprintf("%s.tmp-*", prog.Label)
	prog.tmpDir, err = os.MkdirTemp("", tmpDirTemplate)
	if err != nil {
		return err
	}

	if opts.Env != nil {
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return nil
}

func (prog *RpcConsoleProg) startPipeIO() error {
	cmd := prog.Command
	var err error
	prog.stdin, err = cmd.StdinPipe()
	if err != nil {
		return errors.Context(err, "opening stdin")
	}
	prog.stdout, err = cmd.StdoutPipe()
	if err != nil {
		return errors.Context(err, "opening stdout")
	}
	prog.stderr, err = cmd.StderrPipe()
	if err != nil {
		return errors.Context(err, "opening stderr")
	}

	return prog.Command.Start()
}

func (prog *RpcConsoleProg) resizePty() {
	ws := pty.Winsize{}
	rows, cols, err := pty.Getsize(prog.pty)
	if err != nil {
		log.Printf("Failed to get terminal size: %v", err)
		ws.Rows, ws.Cols = 120, 400
	} else {
		ws.Rows, ws.Cols = uint16(rows), uint16(cols)
	}

	if err = pty.Setsize(prog.pty, &ws); err != nil {
		log.Printf("error resizing pty: %s", err)
	}
}

func (prog *RpcConsoleProg) startPtsIO() error {
	var err error

	prog.pty, err = pty.Start(prog.Command)
	if err != nil {
		return err
	}
	tty := prog.Command.Stdin.(*os.File)
	prog.tty = tty.Name()
	stdinFd := tty.Fd()
	// Make sure to close the pty at the end.
	prog.OnCleanup(func() { // Best effort.
		_ = prog.pty.Close()
	})

	// Handle pty size.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			prog.resizePty()
		}
	}()
	ch <- syscall.SIGWINCH  // Initial resize.
	prog.OnCleanup(func() { // Cleanup signals when done.
		signal.Stop(ch)
		close(ch)
	})

	// Set stdin in raw mode.
	var oldState *term.State
	oldState, err = term.MakeRaw(int(stdinFd))
	if err != nil {
		return errors.Context(err, "term.MakeRaw")
	}
	prog.OnCleanup(func() {
		_ = term.Restore(int(stdinFd), oldState)
	}) // Best effort.

	return nil
}

func (prog *RpcConsoleProg) Start() error {
	var err error
	if prog.AllocPty {
		err = prog.startPtsIO()
	} else {
		return errors.New("Non-PTY console progs not yet implemented")
		//err = prog.startPipeIO()
	}
	if err != nil {
		return errors.Context(err, "starting app")
	}
	fmt.Printf("! Booting app '%s' with command line %s\n", prog.Label, prog.Cmdline)
	prog.eventAdd("booting_app", "cmdline", prog.Cmdline)

	prog.t.Go(prog.watch)
	if prog.IdleTimeout.HasValue() {
		prog.t.Go(prog.idleMonitor)
	}
	prog.t.Go(prog.run)

	err = prog.WaitTilReady()
	if err != nil {
		_ = prog.cleanup()
		return errors.Context(err, "Waiting until ready")
	}
	return nil
}

func (prog *RpcConsoleProg) run() error {
	prog.App.eventAdd("waiting_on_app")

	t := &prog.t
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.Dying():
			prog.eventAdd("dying_on_start")
			fmt.Printf("! Detecting app '%s' dying on start\n", prog.Label)
			return fmt.Errorf("app died before booting")
		case <-ticker.C:
			prog.eventAdd("app_ready")
			fmt.Printf("! App '%s' booted\n", prog.Label)
			close(prog.readyChan)
			return nil
		}
	}
}

func (prog *RpcConsoleProg) watch() error {
	//c := make(chan error)

	//go func() {
	//	r := bufio.NewReader(a.stdout)
	//
	//	for {
	//		line, err := r.ReadString('\n')
	//		if line != "" {
	//			rpcService.handleLog(a, line)
	//			a.lines.Append(line)
	//			a.lastLogLine = line
	//			fmt.Fprintf(os.Stdout, "%s[%d]: %s", a.Name, a.Command.Process.Pid, line)
	//		}
	//
	//		if err != nil {
	//			c <- err
	//			return
	//		}
	//	}
	//}()

	var err error

	//reason := "detected interval shutdown"

	//select {
	//case err = <-c:
	//	reason = "stdout/stderr closed"
	//	err = fmt.Errorf("%s: %s (%s)", ErrUnexpectedExit, prog.Cmdline, prog.Label)
	//case <-prog.t.Dying():
	//	err = nil
	//}

	select {
	case <-prog.t.Dying():
	}
	//prog.Kill(reason)
	//if err != nil {
	//	return errors.Context(err, "killing command")
	//}
	err = prog.Command.Wait()
	if err != nil {
		return errors.Context(err, "waiting for command to exit")
	}
	prog.eventAdd("shutdown")
	defer func() { _ = prog.cleanup() }()

	fmt.Printf("* Console Program '%s' shutdown and cleaned up\n", prog.Label)

	return err
}

func (prog *RpcConsoleProg) IsTimedOut() bool {
	return prog.lastUse.WithValue(func(lastUse time.Time) any {
		diff := time.Since(lastUse)
		return prog.IdleTimeout.HasValue() && diff > *prog.IdleTimeout.Ptr()
	}, false).(bool)

}
func (prog *RpcConsoleProg) idleMonitor() error {
	if !prog.IdleTimeout.HasValue() {
		return nil
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if prog.IsTimedOut() {
				err := prog.Kill("Console program is idle")
				return errors.Context(err, "Killing ConsoleProg after timeout")
			}
		case <-prog.t.Dying():
			return nil
		}
	}
}

//func (prog *RpcConsoleProg) restartMonitor() error {
//	tmpDir := filepath.Join(a.dir, "tmp")
//	err := os.MkdirAll(tmpDir, 0755)
//	if err != nil {
//		return err
//	}
//
//	restart := filepath.Join(tmpDir, "restart.txt")
//
//	f, err := os.Create(restart)
//	if err != nil {
//		return err
//	}
//	f.Close()
//
//	return watch.Watch(restart, a.t.Dying(), func() {
//		a.Kill("restart.txt touched")
//	})
//}

func (prog *RpcConsoleProg) WaitTilReady() error {
	select {
	case <-prog.readyChan:
		// double check we aren't also dying
		select {
		case <-prog.t.Dying():
			return prog.t.Err()
		default:
			prog.lastUse.Set(time.Now())
			return nil
		}
	case <-prog.t.Dying():
		return prog.t.Err()
	}
}

func (prog *RpcConsoleProg) Kill(reason string) error {
	proc := prog.Command.Process
	prog.eventAdd("killing_console_program",
		"reason", reason,
	)

	fmt.Printf("! Killing '%s' (%d) - '%s'\n", prog.Label, proc.Pid, reason)
	err := proc.Signal(syscall.SIGTERM)
	if err != nil {
		prog.eventAdd("killing_error", "error", err.Error())
		fmt.Printf("! Error trying to kill %s: %s", prog.Label, err)
	} else {
		prog.eventAdd("shutdown")
	}

	return err
}

func (prog *RpcConsoleProg) OnCleanup(callback ...func()) {
	prog.cleanupRoutines = append(prog.cleanupRoutines, callback...) // Best effort.
}

func (prog *RpcConsoleProg) cleanup() error {
	if prog.Command.ProcessState != nil {

	}
	for _, callback := range prog.cleanupRoutines {
		callback()
	}
	return nil
}
