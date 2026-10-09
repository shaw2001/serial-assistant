package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// UpdateState is the timestamp file that keeps the startup check quiet for a day.
type UpdateState struct {
	LastCheck time.Time `json:"lastCheck"`
}

// UpdateInfo is what the renderer needs to describe the newest published release.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Published string `json:"published"`
	AssetName string `json:"assetName"`
	AssetSize int64  `json:"assetSize"`
	Available bool   `json:"available"`
	Checked   bool   `json:"checked"`
	Skipped   bool   `json:"skipped"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// CompareVersion orders two dotted versions; missing or malformed parts count as 0.
func CompareVersion(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Newer reports whether latest is a strictly newer release than current.
func Newer(latest, current string) bool {
	return CompareVersion(strings.TrimPrefix(strings.TrimSpace(latest), "v"), strings.TrimPrefix(strings.TrimSpace(current), "v")) > 0
}

func versionParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.Split(v, ".") {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out[i] = n
	}
	return out
}

// ParseRelease reads the GitHub release payload and keeps the first Windows x64 EXE asset.
func ParseRelease(data []byte) (UpdateInfo, error) {
	var r githubRelease
	if e := json.Unmarshal(data, &r); e != nil {
		return UpdateInfo{}, errors.New("GitHub 返回的版本信息无法解析。")
	}
	tag := strings.TrimSpace(r.TagName)
	if !releaseTag.MatchString(tag) || r.Draft || r.Prerelease {
		return UpdateInfo{}, errors.New("GitHub 未返回可用版本。")
	}
	info := UpdateInfo{Latest: tag, Name: strings.TrimSpace(r.Name), URL: "https://github.com/shaw2001/serial-assistant/releases/tag/" + tag, Published: r.PublishedAt, Checked: true}
	if info.URL == "" {
		info.URL = "https://github.com/shaw2001/serial-assistant/releases/latest"
	}
	for _, a := range r.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") && strings.Contains(lower, "win") {
			info.AssetName, info.AssetSize = a.Name, a.Size
			break
		}
	}
	return info, nil
}

// FetchRelease asks GitHub for the newest published release and compares it with current.
func FetchRelease(ctx context.Context, client *http.Client, repo, current string) (UpdateInfo, error) {
	return fetchRelease(ctx, client, "https://api.github.com/repos/"+repo+"/releases/latest", current)
}

func fetchRelease(ctx context.Context, client *http.Client, endpoint, current string) (UpdateInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("更新检查不允许重定向。") }}
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return UpdateInfo{}, e
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SerialAssistant/"+current)
	res, e := client.Do(req)
	if e != nil {
		return UpdateInfo{}, errors.New("无法连接 GitHub，请检查网络后重试。")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
		return UpdateInfo{}, errors.New("GitHub 请求受限，稍后再检查更新。")
	}
	if res.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("GitHub 返回状态 %d。", res.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, 512*1024))
	if e != nil {
		return UpdateInfo{}, e
	}
	info, e := ParseRelease(body)
	if e != nil {
		return UpdateInfo{}, e
	}
	return Compare(info, current), nil
}

// Compare fills the current version and the up-to-date flag for a parsed release.
func Compare(info UpdateInfo, current string) UpdateInfo {
	info.Current = current
	info.Available = Newer(info.Latest, current)
	return info
}

// LoadUpdateState reads the throttle timestamp; a missing file is not an error.
func LoadUpdateState(file string) UpdateState {
	var s UpdateState
	data, e := os.ReadFile(file)
	if e == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// SaveUpdateState stores the throttle timestamp atomically.
func SaveUpdateState(file string, s UpdateState) error {
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		return e
	}
	data, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return AtomicWrite(file, data)
}

// ShouldCheck reports whether enough time passed since the last successful check.
func ShouldCheck(s UpdateState, now time.Time, interval time.Duration) bool {
	return s.LastCheck.IsZero() || now.Sub(s.LastCheck) >= interval
}

// Only the application's official HTTPS release pages may leave the app.
func ValidReleaseURL(target string) bool {
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	const prefix = "/shaw2001/serial-assistant/releases/"
	if u.Path == prefix+"latest" {
		return true
	}
	tag := strings.TrimPrefix(u.Path, prefix+"tag/")
	return u.Path != tag && releaseTag.MatchString(tag)
}

var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
