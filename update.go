package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Updates, as vps2cloud does them. The latest release is read at most once an hour and remembered in the config file:
// from dns.1ms.dog/v, which asks GitHub and caches the answer, or else from GitHub's redirect for /releases/latest.
// Release files come the same way: dns.1ms.dog/dl/<tag>/ passes GitHub's on, since GitHub is often unreachable from
// mainland China. In a terminal the tool offers the update and, when the user agrees, downloads the build for this
// system, checks it against the release's checksums.txt, puts it in place of the running binary and starts it again
// with the same options. Without a terminal it only mentions the new version.
// --offline, DNSTEST_NO_UPDATE=1 and development builds skip all of it; --update checks and updates right away.

var (
	releaseURL = "https://github.com/yt1988/dns-test-cli/releases"
	mirrorURL  = "https://dns.1ms.dog"
)

const checkEvery = time.Hour

func userAgent() string {
	return "dnstest/" + version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}

func latestVersion() (string, error) {
	if v, err := mirrorVersion(); err == nil {
		return v, nil
	}
	return githubVersion()
}

func mirrorVersion() (string, error) {
	req, _ := http.NewRequest("GET", mirrorURL+"/v", nil)
	req.Header.Set("User-Agent", userAgent())
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64))
	v := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
	if res.StatusCode != 200 || parseVersion(v) == nil {
		return "", fmt.Errorf("%s/v: %s", mirrorURL, res.Status)
	}
	return v, nil
}

func githubVersion() (string, error) {
	cl := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("HEAD", releaseURL+"/latest", nil)
	req.Header.Set("User-Agent", userAgent())
	res, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	tag := path.Base(res.Header.Get("Location"))
	if !strings.HasPrefix(tag, "v") || parseVersion(tag[1:]) == nil {
		return "", fmt.Errorf("no release tag in %q", res.Header.Get("Location"))
	}
	return tag[1:], nil
}

// parseVersion reads "1.2.3"; anything else (dev builds) is nil.
func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// newer tells whether version a is later than b; false when either isn't a release version.
func newer(a, b string) bool {
	x, y := parseVersion(a), parseVersion(b)
	if x == nil || y == nil {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// assetName is this system's file in a release, as the release workflow names it.
func assetName() string {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	name := "dnstest-" + runtime.GOOS + "-" + arch
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func download(url string, max int64) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", userAgent())
	res, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, max))
}

var errPermission = errors.New("permission")

// fetchRelease gets this system's build of release ver and the release's checksums, from the mirror or else GitHub.
func fetchRelease(ver, name string) (bin, sums []byte, err error) {
	for _, base := range []string{mirrorURL + "/dl/v" + ver + "/", releaseURL + "/download/v" + ver + "/"} {
		if bin, err = download(base+name, 64<<20); err != nil {
			continue
		}
		if sums, err = download(base+"checksums.txt", 64<<10); err == nil {
			return bin, sums, nil
		}
	}
	return nil, nil, err
}

// selfUpdate replaces the running binary with release ver and returns its path.
func selfUpdate(ver string) (string, error) {
	name := assetName()
	bin, sums, err := fetchRelease(ver, name)
	if err != nil {
		return "", err
	}
	want := ""
	for _, ln := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(ln); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	got := sha256.Sum256(bin)
	if want == "" || hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("checksum mismatch for %s", name)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	// write next to the binary, then rename over it: the running program keeps its old file until it exits
	tmp := filepath.Join(filepath.Dir(exe), ".dnstest-update")
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return exe, errPermission
		}
		return "", err
	}
	if runtime.GOOS == "windows" {
		// a running .exe cannot be overwritten, but it can be renamed out of the way
		os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			os.Remove(tmp)
			return "", err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		if errors.Is(err, os.ErrPermission) {
			return exe, errPermission
		}
		return "", err
	}
	return exe, nil
}

// readAnswer reads one line from stdin a byte at a time, so nothing typed after it is lost to a buffer.
func readAnswer() string {
	var b bytes.Buffer
	one := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(one)
		if n == 0 || err != nil || one[0] == '\n' {
			break
		}
		b.WriteByte(one[0])
	}
	return strings.TrimSpace(b.String())
}

// justLooking tells whether the options only ask for the version, the help or the list, which shouldn't stop to
// offer an update.
func justLooking(args []string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue // a value, such as -r list
		}
		switch strings.TrimLeft(a, "-") {
		case "V", "version", "h", "help", "list":
			return true
		}
	}
	return false
}

// checkUpdate runs before anything else. It returns an exit code when the program should stop (after --update).
func checkUpdate(args []string) (int, bool) {
	force := len(args) == 1 && args[0] == "--update"
	if !force && (parseVersion(version) == nil || os.Getenv("DNSTEST_NO_UPDATE") != "" || contains(args, "--offline") || contains(args, "-offline") || justLooking(args)) {
		return 0, false
	}
	if force && parseVersion(version) == nil {
		fmt.Fprintln(os.Stderr, "dnstest: a development build does not update itself")
		return 1, true
	}
	useColor = os.Getenv("NO_COLOR") == "" && isTTY(os.Stderr) && enableVT()
	c := loadConfig()
	latest := c.Latest
	if force || time.Since(time.Unix(c.CheckedAt, 0)) > checkEvery {
		if v, err := latestVersion(); err == nil {
			latest, c.Latest, c.CheckedAt = v, v, time.Now().Unix()
			saveConfig(c)
		} else if force {
			fmt.Fprintln(os.Stderr, "dnstest: "+T("updFail", err.Error()))
			return 1, true
		}
	}
	if !newer(latest, version) {
		if force {
			fmt.Fprintln(os.Stderr, T("updLatest", version))
			return 0, true
		}
		return 0, false
	}
	if !force {
		if !isTTY(os.Stdin) || !isTTY(os.Stderr) {
			fmt.Fprintln(os.Stderr, paint(cDim, T("updNote", latest)))
			return 0, false
		}
		fmt.Fprintf(os.Stderr, "%s %s ", paint(cYellow, "↑ "+T("updAsk", latest, version)), paint(cDim, "[Y/n]"))
		if a := strings.ToLower(readAnswer()); a != "" && !strings.HasPrefix(a, "y") {
			return 0, false
		}
	}
	exe, err := selfUpdate(latest)
	if err == errPermission {
		fmt.Fprintln(os.Stderr, paint(cYellow, "dnstest: "+T("updPerm", exe)))
		if force {
			return 1, true
		}
		return 0, false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, paint(cYellow, "dnstest: "+T("updFail", err.Error())))
		if force {
			return 1, true
		}
		return 0, false
	}
	fmt.Fprintln(os.Stderr, paint(cGreen, "✓ "+T("updDone", latest)))
	if force {
		return 0, true
	}
	// start the new binary with the same options; where that isn't possible, ask to run it again
	if err := reexec(exe, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, T("updRestart"))
		return 0, true
	}
	return 0, false
}
