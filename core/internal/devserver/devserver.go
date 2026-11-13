// Package devserver starts the development server of a local project when the assistant needs to see its pages (to
// take a screenshot), waits until it answers, and stops it when it is no longer used.
//
// The assistant cannot do it itself: its sandbox forbids running a server, and it would have to ask the user to start it
// and to say when it is ready. Pouple does it, outside the sandbox. The assistant names what to start, and the usual
// ways to start a dev server (a script such as dev or preprod, vite, next, python -m http.server…) are run as they are.
// Anything else (an unusual script, a shell line, a server open to the network) is run only after the user allowed that
// exact command: Serve then returns an *ApprovalError.
package devserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

const (
	// startTimeout is how long a server may take to answer: installing, compiling, a first bundle.
	startTimeout = 150 * time.Second
	// idleTimeout stops a server nobody asked a picture of for a while.
	idleTimeout = 30 * time.Minute
	// maxServers bounds how many servers run at once.
	maxServers = 2
	maxLog     = 4096
)

var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ApprovalError says the command is not one of the usual ways to start a dev server: the user must allow it first.
type ApprovalError struct {
	// Command is exactly what would run.
	Command string
	// Why says in a few words what makes it unusual.
	Why string
}

func (e *ApprovalError) Error() string {
	return "this command needs the user's approval: " + e.Command + " (" + e.Why + ")"
}

// Plan is what will be run, and whether it is one of the usual ways to start a dev server.
type Plan struct {
	// Argv is run directly, with no shell to read it: set for the usual ways.
	Argv []string
	// Shell is a line read by the login shell: set only for what the user allowed.
	Shell string
	// Label is shown to the user and kept in the journal.
	Label string
	// Unusual is set when the user must allow it; Why says why.
	Unusual bool
	Why     string
}

// Served is a server that answers.
type Served struct {
	// URL is the address of its root, such as http://localhost:8001.
	URL string
	// Command is what was started, for the journal ("pnpm run preprod").
	Command string
	// Reused is set when the server was already running.
	Reused bool
}

// Manager starts and keeps the servers.
type Manager struct {
	// Root returns the folder that holds the projects.
	Root func() string
	// Enabled says whether Pouple may start servers at all.
	Enabled func() bool
	// Shell is the login shell the command runs in, so a Node installed with nvm is found like in a terminal.
	Shell string
	// Ready tells that a port answers; the real check unless a test replaces it.
	Ready func(ctx context.Context, port int) bool
	// Listening lists the addresses a process and its descendants listen on; lsof unless a test replaces it.
	Listening func(ctx context.Context, pid int) []Listener
	// Start builds the command; the real one unless a test replaces it.
	Start func(dir string, plan Plan) *exec.Cmd

	mu       sync.Mutex
	servers  map[string]*server
	starting int
	locks    map[string]*sync.Mutex
}

// lock serializes the starts of one server: two conversations asking for the same one start it once.
func (m *Manager) lock(key string) func() {
	m.mu.Lock()
	if m.locks == nil {
		m.locks = map[string]*sync.Mutex{}
	}
	l := m.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

type server struct {
	project, command, url string
	process               *exec.Cmd
	group                 int
	exited                chan struct{}
	log                   *ring
	idle                  *time.Timer
}

// ring keeps the end of what a server wrote, to say why it failed.
type ring struct {
	mu   sync.Mutex
	data []byte
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, p...)
	if len(r.data) > maxLog {
		r.data = r.data[len(r.data)-maxLog:]
	}
	return len(p), nil
}

func (r *ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(string(r.data))
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// tail is the last lines of a server's output, without colors, short enough to read.
func tail(text string) string {
	text = ansi.ReplaceAllString(text, "")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > 1200 {
		out = out[len(out)-1200:]
	}
	return out
}

// New builds a manager on the real tools.
func New(root func() string, enabled func() bool, shell string) *Manager {
	return &Manager{Root: root, Enabled: enabled, Shell: shell}
}

func (m *Manager) ready(ctx context.Context, port int) bool {
	if m.Ready != nil {
		return m.Ready(ctx, port)
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+strconv.Itoa(port)+"/", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return true
}

// Listener is a port a server listens on.
type Listener struct {
	Port int
	// Exposed is set when it listens on every network interface, not on this computer alone.
	Exposed bool
}

func (m *Manager) listening(ctx context.Context, group int) []Listener {
	if m.Listening != nil {
		return m.Listening(ctx, group)
	}
	return listeningPorts(ctx, group)
}

// descendants are the process and everything it started, however deep and whatever group they put themselves in: the
// package manager hands the script to other processes, and a server may detach from its group.
func descendants(ctx context.Context, root int) []int {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return []int{root}
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		parent, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			children[parent] = append(children[parent], pid)
		}
	}
	all := []int{root}
	for i := 0; i < len(all); i++ {
		all = append(all, children[all[i]]...)
	}
	return all
}

// listeningPorts asks lsof for the TCP ports the process and its descendants listen on.
func listeningPorts(ctx context.Context, root int) []Listener {
	pids := descendants(ctx, root)
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-p", strings.Join(list, ","), "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var ports []Listener
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		host, port, err := net.SplitHostPort(strings.TrimPrefix(line, "n"))
		number, convErr := strconv.Atoi(port)
		// Debugger ports and privileged ones are never the page the assistant wants.
		if err != nil || convErr != nil || number < 1024 || number == 9229 || number == 9230 {
			continue
		}
		if !slices.ContainsFunc(ports, func(l Listener) bool { return l.Port == number }) {
			ports = append(ports, Listener{Port: number, Exposed: !isLoopback(host)})
		}
	}
	return ports
}

// packageManager is the one the project's lock file names.
func packageManager(dir string) string {
	for _, candidate := range []struct{ file, manager string }{
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"},
	} {
		if _, err := os.Stat(filepath.Join(dir, candidate.file)); err == nil {
			return candidate.manager
		}
	}
	return "npm"
}

// projectDir is the folder of a project under the projects root: a plain folder name, a real folder, nothing that leads
// out of the root.
func (m *Manager) projectDir(project string) (string, error) {
	root := m.Root()
	if root == "" || !projectName.MatchString(project) {
		return "", i18n.NewError("“%s” is not a project of your projects folder", project)
	}
	dir := filepath.Join(root, project)
	resolved, err := filepath.EvalSymlinks(dir)
	rootResolved, rootErr := filepath.EvalSymlinks(root)
	if err != nil || rootErr != nil || filepath.Dir(resolved) != rootResolved {
		return "", i18n.NewError("“%s” is not a project of your projects folder", project)
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return "", i18n.NewError("“%s” is not a project of your projects folder", project)
	}
	return resolved, nil
}

// usualScripts are the scripts whose job is to start a dev server, by name.
var usualScript = regexp.MustCompile(`^(dev|develop|start|serve|preview|preprod|storybook)(:[a-z0-9_-]+)?$`)

// devTools are the dev servers of the usual front-end tools.
var devTools = []string{"vite", "next", "nuxt", "astro", "ng", "webpack", "webpack-dev-server", "parcel", "storybook", "remix", "svelte-kit", "vue-cli-service", "rspack", "rsbuild"}

var (
	scriptWord = regexp.MustCompile(`^[a-z][a-z0-9:_-]{0,60}$`)
	shellMeta  = regexp.MustCompile("[;&|<>`$()\\\n\r\"'{}*?!~]")
)

func manifestScripts(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil || len(data) > 1<<20 {
		return nil, i18n.NewError("This project has no package.json to start a server from")
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil, i18n.NewError("This project has no package.json to start a server from")
	}
	return manifest.Scripts, nil
}

// exposed says whether the command opens the server to the network instead of this computer alone.
func exposed(args []string) bool {
	for i, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case lower == "--host" || lower == "-h":
			if i+1 < len(args) && !isLoopback(args[i+1]) {
				return true
			}
			if i+1 >= len(args) {
				return true // vite --host alone means every interface
			}
		case strings.HasPrefix(lower, "--host="):
			if !isLoopback(strings.TrimPrefix(lower, "--host=")) {
				return true
			}
		case strings.Contains(lower, "0.0.0.0"), lower == "::":
			return true
		}
	}
	return false
}

func isLoopback(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func isDevTool(name string) bool {
	return slices.Contains(devTools, strings.TrimPrefix(name, "./node_modules/.bin/"))
}

// plan decides what to run for what the assistant asked, and whether the user must allow it. wanted is "" (the
// project's usual script), a script name, or a whole command.
func (m *Manager) plan(dir, wanted string) (Plan, error) {
	scripts, err := manifestScripts(dir)
	wanted = strings.TrimSpace(wanted)
	manager := packageManager(dir)

	if wanted == "" || scriptWord.MatchString(wanted) {
		if err != nil {
			return Plan{}, err
		}
		if wanted == "" {
			for _, name := range []string{"dev", "develop", "start", "serve", "preprod", "preview", "storybook"} {
				if _, ok := scripts[name]; ok {
					wanted = name
					break
				}
			}
			if wanted == "" {
				return Plan{}, i18n.NewError("This project has none of the scripts %s", "dev, start, serve, preprod, preview")
			}
		}
		if _, ok := scripts[wanted]; !ok {
			return Plan{}, i18n.NewError("This project has no “%s” script", wanted)
		}
		plan := Plan{Argv: []string{manager, "run", wanted}, Label: manager + " run " + wanted}
		if !usualScript.MatchString(wanted) {
			plan.Unusual, plan.Why = true, "a script that is not a dev server by its name"
		}
		return plan, nil
	}

	if len(wanted) > 300 || strings.ContainsAny(wanted, "\x00\x1b") {
		return Plan{}, i18n.NewError("This command is not valid")
	}
	// A line that needs a shell to be read (pipes, variables, chains) is never run unless the user allowed it.
	if shellMeta.MatchString(wanted) {
		return Plan{Shell: wanted, Label: wanted, Unusual: true, Why: "a shell line"}, nil
	}
	argv := strings.Fields(wanted)
	// VAR=value before the command is read by a shell, not by exec.
	if strings.Contains(argv[0], "=") {
		return Plan{Shell: wanted, Label: wanted, Unusual: true, Why: "a variable set before the command"}, nil
	}
	plan := Plan{Argv: argv, Label: strings.Join(argv, " ")}
	switch {
	case slices.Contains([]string{"pnpm", "npm", "yarn", "bun"}, argv[0]):
		plan.Unusual, plan.Why = !usualPackageCommand(argv, scripts), "not a usual way to start a dev server"
	case isDevTool(argv[0]):
	case (argv[0] == "python3" || argv[0] == "python") && len(argv) >= 3 && argv[1] == "-m" && argv[2] == "http.server":
	default:
		plan.Unusual, plan.Why = true, "not a usual way to start a dev server"
	}
	if !plan.Unusual && exposed(argv[1:]) {
		plan.Unusual, plan.Why = true, "it opens the server to the network"
	}
	return plan, nil
}

// usualPackageCommand: `pnpm dev`, `npm run dev`, `yarn start`, `pnpm exec vite --port 8001`.
func usualPackageCommand(argv []string, scripts map[string]string) bool {
	switch {
	case len(argv) == 2:
		return usualScript.MatchString(argv[1])
	case len(argv) == 3 && argv[1] == "run":
		_, exists := scripts[argv[2]]
		return exists && usualScript.MatchString(argv[2])
	case len(argv) >= 3 && (argv[1] == "exec" || argv[1] == "x"):
		return isDevTool(argv[2])
	}
	return false
}

func (m *Manager) command(dir string, plan Plan) *exec.Cmd {
	if m.Start != nil {
		return m.Start(dir, plan)
	}
	shell := m.Shell
	if shell == "" {
		shell = "/bin/zsh"
	}
	var command *exec.Cmd
	if plan.Shell != "" {
		// Only what the user allowed, exactly as they read it.
		command = exec.Command(shell, "-lc", plan.Shell)
	} else {
		// The login shell finds the Node of the user's own setup; the words are data, never code.
		command = exec.Command(shell, append([]string{"-lc", `exec "$0" "$@"`}, plan.Argv...)...)
	}
	command.Dir = dir
	command.Env = providers.ChildEnv(os.Environ(), "BROWSER=none", "NO_COLOR=1", "FORCE_COLOR=0", "HOST=127.0.0.1", "HOSTNAME=127.0.0.1")
	return command
}

// startFiles are the files that decide what starting a project's server runs.
var startFiles = regexp.MustCompile(`(^|/)(package\.json|\.npmrc|\.yarnrc(\.yml)?|\.pnpmfile\.cjs|pnpm-workspace\.yaml|(vite|webpack|next|nuxt|astro|rsbuild|rspack|svelte|vue)\.config\.[a-z]+|\.env[^/]*)$`)

// changedStartFiles lists the start files with changes git does not know of yet; nothing when the project is not a git
// repository or git is missing.
func changedStartFiles(ctx context.Context, dir string) []string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain", "--untracked-files=normal").Output()
	if err != nil {
		return nil
	}
	var changed []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) > 3 {
			if path := strings.TrimSpace(line[3:]); startFiles.MatchString(path) && !slices.Contains(changed, path) {
				changed = append(changed, path)
			}
		}
	}
	return changed
}

// Serve starts the dev server of a project (or finds the one already running) and waits until it answers. say is told
// what is happening, for the user to read. The wait is Pouple's: the assistant is not asked to come back later.
func (m *Manager) Serve(ctx context.Context, project, wanted string, allowed bool, say func(string)) (Served, error) {
	if m.Enabled != nil && !m.Enabled() {
		return Served{}, i18n.NewError("Starting a project's server is turned off in Pouple's settings")
	}
	dir, err := m.projectDir(project)
	if err != nil {
		return Served{}, err
	}
	plan, err := m.plan(dir, wanted)
	if err != nil {
		return Served{}, err
	}
	// What the usual way would run may itself have been changed since the last commit (by the assistant, with the user's
	// approval of another change): the user then sees what is about to run.
	if !plan.Unusual {
		if changed := changedStartFiles(ctx, dir); len(changed) > 0 {
			plan.Unusual, plan.Why = true, "the start configuration has uncommitted changes ("+strings.Join(changed, ", ")+")"
		}
	}
	if plan.Unusual && !allowed {
		return Served{}, &ApprovalError{Command: plan.Label, Why: plan.Why}
	}
	label := plan.Label
	key := dir + "\x00" + label
	defer m.lock(key)()

	if existing := m.running(key); existing != nil {
		m.touch(existing)
		return Served{URL: existing.url, Command: existing.command, Reused: true}, nil
	}
	m.mu.Lock()
	if len(m.servers)+m.starting >= maxServers {
		m.mu.Unlock()
		return Served{}, i18n.NewError("Too many servers are running already: %d", maxServers)
	}
	m.starting++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.starting--
		m.mu.Unlock()
	}()

	if say != nil {
		say(fmt.Sprintf("%s (%s)", project, label))
	}
	command := m.command(dir, plan)
	logs := &ring{}
	command.Stdout, command.Stderr = logs, logs
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return Served{}, i18n.Wrap(err, "The server could not be started: %s", err)
	}
	s := &server{project: project, command: label, process: command, group: command.Process.Pid, exited: make(chan struct{}), log: logs}
	go func() {
		_ = command.Wait()
		close(s.exited)
	}()

	port, err := m.waitUntilListening(ctx, s)
	if err != nil {
		m.stop(s)
		return Served{}, err
	}
	s.url = "http://localhost:" + strconv.Itoa(port)
	m.mu.Lock()
	if m.servers == nil {
		m.servers = map[string]*server{}
	}
	m.servers[key] = s
	m.mu.Unlock()
	m.touch(s)
	return Served{URL: s.url, Command: label}, nil
}

// waitUntilListening waits for the server to listen on a port and to answer on it, or to fail.
func (m *Manager) waitUntilListening(ctx context.Context, s *server) (int, error) {
	timeout := time.NewTimer(startTimeout)
	defer timeout.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.exited:
			return 0, i18n.NewError("The server stopped before it answered:\n%s", tail(s.log.String()))
		case <-timeout.C:
			return 0, i18n.NewError("The server did not answer within %d seconds:\n%s", int(startTimeout.Seconds()), tail(s.log.String()))
		case <-tick.C:
			for _, listener := range m.listening(ctx, s.group) {
				if m.ready(ctx, listener.Port) {
					if listener.Exposed {
						return 0, i18n.NewError("The server listens on every network interface of this Mac, which would show the project's files to the network: start it for localhost only (for example with --host 127.0.0.1)")
					}
					return listener.Port, nil
				}
			}
		}
	}
}

// running is the server already started for this project and script, when it still answers.
func (m *Manager) running(key string) *server {
	m.mu.Lock()
	s := m.servers[key]
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	select {
	case <-s.exited:
		m.mu.Lock()
		delete(m.servers, key)
		m.mu.Unlock()
		return nil
	default:
		return s
	}
}

// touch keeps a server alive for another idle period.
func (m *Manager) touch(s *server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(idleTimeout, func() { m.stop(s) })
}

// stop ends a server and everything it started.
func (m *Manager) stop(s *server) {
	m.mu.Lock()
	for key, other := range m.servers {
		if other == s {
			delete(m.servers, key)
		}
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	m.mu.Unlock()
	if s.process != nil && s.process.Process != nil {
		// Everything it started goes with it, even what left its group.
		tree := descendants(context.Background(), s.group)
		signal := func(sig syscall.Signal) {
			_ = syscall.Kill(-s.group, sig)
			for _, pid := range tree {
				_ = syscall.Kill(pid, sig)
			}
		}
		signal(syscall.SIGTERM)
		select {
		case <-s.exited:
		case <-time.After(3 * time.Second):
			signal(syscall.SIGKILL)
		}
	}
}

// Shutdown stops every server: none outlives Pouple.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	all := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		m.stop(s)
	}
}
