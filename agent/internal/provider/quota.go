package provider

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
)

// quotaSupport caches whether the engine's storage root sits on a filesystem
// with project-quota support. The overlay driver enforces a per-container disk
// limit ("size" storage option) via project quotas; without them the option is
// silently ignored and `df -h` inside a container shows the host disk instead
// of the limit. XFS exposes project quotas as `pquota`, ext4 as `prjquota`.
type quotaSupport struct {
	once sync.Once
	ok   bool
}

// QuotaSupported reports whether the engine storage filesystem supports
// per-container disk limits via project quotas. Used by the agent at startup
// to warn when the size limit cannot be enforced.
func (p *ociProvider) QuotaSupported(ctx context.Context) bool {
	return p.quota.supported(ctx, p)
}

// supported reports whether the storage filesystem carries a project-quota
// mount option. It is safe to call concurrently; the engine is only queried
// on the first call.
func (q *quotaSupport) supported(ctx context.Context, p *ociProvider) bool {
	q.once.Do(func() {
		var info struct {
			DockerRootDir string `json:"DockerRootDir"`
		}
		if err := p.do(ctx, http.MethodGet, "/info", nil, &info); err != nil {
			return
		}
		opts := mountOptionsContaining(info.DockerRootDir)
		q.ok = opts != "" && (strings.Contains(opts, "pquota") || strings.Contains(opts, "prjquota"))
	})
	return q.ok
}

// mountOptionsContaining returns the mount options of the most specific mount
// point that is a parent of path, read from /proc/self/mounts. Returns "" when
// no mount covers path.
func mountOptionsContaining(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return ""
	}
	best, bestLen := "", -1
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		mnt := decodeMountPath(f[1])
		if path == mnt || strings.HasPrefix(path, mnt+"/") {
			if len(mnt) > bestLen {
				best, bestLen = f[3], len(mnt)
			}
		}
	}
	return best
}

func decodeMountPath(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, "\\")
	return r.Replace(s)
}
